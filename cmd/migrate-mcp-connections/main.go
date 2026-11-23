// Command migrate-mcp-connections copies one AIQG tenant's linked Aether
// organization's PostgreSQL connections into AIQG's shared MCP connection
// store (Plan #22 C.6). See internal/mcpmigrate for the rules.
//
// Usage:
//
//	AIQG_BEARER=<aiqg-ui access token> migrate-mcp-connections \
//	    -aiqg-url http://aiqg-dashboard-be.aiqg.svc.cluster.local:8095 \
//	    [-server-id postgres-mcp] [-database-id <aether db id>] [-apply]
//
// The token is an AIQG sign-in (client aiqg-ui, realm aether) of a user who is
// an owner or admin of the target org and holds aiqg-platform-operator. Its
// tenant_id claim is the tenant written to. Access tokens last 5 minutes, so
// mint one just before the run. It is read from the environment, never a
// flag, so it does not reach the process list or shell history.
//
// Without -apply it only prints the plan. Neo4j settings come from the usual
// aether-be environment (NEO4J_URI, NEO4J_USERNAME, NEO4J_PASSWORD). Secrets
// are read in-cluster, or from KUBECONFIG / ~/.kube/config outside one.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/mcpmigrate"
)

func main() {
	aiqgURL := flag.String("aiqg-url", "http://aiqg-dashboard-be.aiqg.svc.cluster.local:8095", "aiqg-dashboard-be base URL")
	serverID := flag.String("server-id", "postgres-mcp", "proxy server id the connections are for")
	dbID := flag.String("database-id", "", "the Aether connection to copy when the org has several")
	apply := flag.Bool("apply", false, "write to AIQG and Neo4j (default: dry run)")
	flag.Parse()

	if err := run(*aiqgURL, *serverID, *dbID, *apply); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(aiqgURL, serverID, dbID string, apply bool) error {
	bearer := os.Getenv("AIQG_BEARER")
	if bearer == "" {
		return fmt.Errorf("set AIQG_BEARER to an AIQG access token")
	}
	tenant, err := mcpmigrate.TenantFromToken(bearer)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log, err := logger.New(logger.Config{Level: "WARN", Format: "json"})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	neo, err := database.NewNeo4jClient(cfg.Neo4j, log)
	if err != nil {
		return fmt.Errorf("connect to Neo4j: %w", err)
	}
	defer neo.Close(ctx)

	k8s, err := kubeClient()
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	rep, err := mcpmigrate.Run(ctx, mcpmigrate.Neo4jGraph{DB: neo}, mcpmigrate.SecretCredentials{K8s: k8s},
		mcpmigrate.HTTPAIQG{BaseURL: aiqgURL, Bearer: bearer},
		mcpmigrate.Options{TenantID: tenant, ServerID: serverID, DatabaseID: dbID, Apply: apply})
	if err != nil {
		return err
	}
	print(rep)
	if rep.Failed() {
		os.Exit(2)
	}
	return nil
}

func kubeClient() (kubernetes.Interface, error) {
	rc, err := rest.InClusterConfig()
	if err != nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		rc, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(rc)
}

func print(r *mcpmigrate.Report) {
	mode := "DRY RUN (no changes; pass -apply to write)"
	if r.Apply {
		mode = "APPLY"
	}
	fmt.Printf("%s\nAIQG tenant %s <- Aether org %s (Aether tenant %s), server %s\n\n",
		mode, r.TenantID, r.AetherOrgID, r.AetherTenantID, r.ServerID)
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "DATABASE ID\tNAME\tHOST\tDB\tACTION\tAPPLIED\tCONNECTION\tREASON")
	for _, s := range r.Steps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t%s\t%s\n", s.DatabaseID, s.Name, s.Host, s.Database, s.Action, s.Applied, s.ConnectionID, s.Reason)
	}
	_ = w.Flush()
	if len(r.Steps) == 0 {
		fmt.Println("no PostgreSQL connections in this organization")
	}
}

//go:build integration

// Package integration — tenant isolation (TEST-3, TEST-2).
//
// Two tenants, A and B, each with their own notebook, document, conversation,
// messages and comments. Every method that was fixed for TEST-3 is called with
// tenant A's context and tenant B's resource id, and must return nothing.
//
// Both halves matter. The negative half catches a missing filter; the positive
// half — tenant A reading tenant A's own rows — catches a filter that is too
// tight, which is the failure mode that takes a feature down rather than
// leaking data. TEST-3 claimed notebook and document were "covered" since
// 2026-08-13 and neither half had ever been asserted.
//
// This talks to the service layer against a real Neo4j rather than going
// through HTTP, because the tenant filters live in the service layer and
// Cypher behaviour is exactly what is under test — a mock would prove nothing
// about whether a WHERE clause is present. CI already runs a
// neo4j:5.15-community service container (.github/workflows/test.yml), so no
// new infrastructure is needed.
//
// It skips, rather than fails, when Neo4j is unreachable. The rest of
// tests/integration/ hard-fails without a live aether-be on :8080, which is
// why those five suites are red in any environment that has not started the
// whole stack; this file deliberately does not add a sixth.
//
// Run with: make test-integration   (or: go test -tags=integration ./tests/integration/ -run TenantIsolation)
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

// tenantFixture is one tenant's worth of seeded data.
type tenantFixture struct {
	tenant     string
	space      string
	userID     string
	notebookID string
	documentID string
	convID     string
	commentID  string
	spaceCtx   *models.SpaceContext
}

type isolationEnv struct {
	neo4j     *database.Neo4jClient
	notebooks *services.NotebookService
	convs     *services.ConversationService
	comments  *services.CommentService
	docs      *services.DocumentService
	A, B      tenantFixture
}

func neo4jURI() string {
	if v := os.Getenv("NEO4J_URI"); v != "" {
		return v
	}
	return "bolt://localhost:7687"
}

func neo4jPassword() string {
	if v := os.Getenv("NEO4J_PASSWORD"); v != "" {
		return v
	}
	return "password"
}

// setupIsolationEnv connects to Neo4j and seeds two fully populated tenants.
func setupIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()

	log, err := logger.New(logger.Config{Level: "error", Format: "console"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	client, err := database.NewNeo4jClient(config.DatabaseConfig{
		URI:      neo4jURI(),
		Username: "neo4j",
		Password: neo4jPassword(),
		Database: "neo4j",
		MaxConns: 8,
	}, log)
	if err != nil {
		t.Skipf("Neo4j not reachable at %s (%v) — skipping tenant isolation tests. "+
			"CI provides a neo4j:5.15-community service container; locally, start one or "+
			"port-forward: kubectl port-forward -n aether-be svc/neo4j 7687:7687", neo4jURI(), err)
	}

	env := &isolationEnv{neo4j: client}
	env.notebooks = services.NewNotebookService(client, log)
	env.convs = services.NewConversationService(client, env.notebooks, log)
	env.comments = services.NewCommentService(client, env.notebooks, log)
	env.docs = services.NewDocumentService(client, env.notebooks, log)

	// A unique run id keeps concurrent or repeated runs from colliding, and makes
	// the teardown below able to delete exactly what this run created.
	run := fmt.Sprintf("test-%d", time.Now().UnixNano())
	env.A = env.seedTenant(t, run, "a")
	env.B = env.seedTenant(t, run, "b")

	t.Cleanup(func() {
		ctx := context.Background()
		_, err := client.ExecuteQuery(ctx, `
			MATCH (n)
			WHERE n.tenant_id STARTS WITH $run OR n.id STARTS WITH $run
			DETACH DELETE n
		`, map[string]interface{}{"run": run})
		if err != nil {
			t.Logf("cleanup failed for run %s: %v", run, err)
		}
		client.Close(ctx)
	})

	return env
}

// seedTenant writes one tenant's User, Notebook, Document, ChatConversation,
// ChatMessage and Comment directly, so the fixture does not depend on the very
// create paths under test.
func (e *isolationEnv) seedTenant(t *testing.T, run, suffix string) tenantFixture {
	t.Helper()
	ctx := context.Background()

	f := tenantFixture{
		tenant:     fmt.Sprintf("%s-tenant-%s", run, suffix),
		space:      fmt.Sprintf("%s-space-%s", run, suffix),
		userID:     fmt.Sprintf("%s-user-%s", run, suffix),
		notebookID: fmt.Sprintf("%s-notebook-%s", run, suffix),
		documentID: fmt.Sprintf("%s-document-%s", run, suffix),
		convID:     fmt.Sprintf("%s-conv-%s", run, suffix),
		commentID:  fmt.Sprintf("%s-comment-%s", run, suffix),
	}
	f.spaceCtx = &models.SpaceContext{
		SpaceType:   models.SpaceTypePersonal,
		SpaceID:     f.space,
		TenantID:    f.tenant,
		UserID:      f.userID,
		UserRole:    "owner",
		ResolvedAt:  time.Now(),
		Permissions: []string{"read", "write", "create", "update", "delete"},
	}

	params := map[string]interface{}{
		"tenant": f.tenant, "space": f.space, "user": f.userID,
		"notebook": f.notebookID, "document": f.documentID,
		"conv": f.convID, "comment": f.commentID,
		"now": time.Now().Format(time.RFC3339),
	}

	_, err := e.neo4j.ExecuteQuery(ctx, `
		CREATE (u:User {id: $user, keycloak_id: $user, email: $user + '@example.test',
		                username: $user, full_name: 'Isolation Test ' + $user,
		                personal_tenant_id: $tenant, created_at: datetime($now)})
		CREATE (sp:Space {id: $space, tenant_id: $tenant, name: 'Space ' + $space,
		                  space_type: 'personal', created_at: datetime($now)})
		CREATE (n:Notebook {id: $notebook, name: 'Notebook ' + $notebook, description: 'seed',
		                    visibility: 'private', status: 'active', owner_id: $user,
		                    space_type: 'personal', space_id: $space, tenant_id: $tenant,
		                    document_count: 1, total_size_bytes: 10, tags: [],
		                    search_text: 'notebook', created_at: datetime($now),
		                    updated_at: datetime($now)})
		CREATE (d:Document {id: $document, name: 'secret-' + $tenant + '.pdf',
		                    original_name: 'secret.pdf', description: 'seed', type: 'pdf',
		                    status: 'processed', mime_type: 'application/pdf', size_bytes: 10,
		                    notebook_id: $notebook, owner_id: $user, tags: [],
		                    space_type: 'personal', space_id: $space, tenant_id: $tenant,
		                    url: 'https://example.test/shared-name.pdf',
		                    storage_path: 'shared/path.pdf',
		                    processing_job_id: 'shared-job-id',
		                    created_at: datetime($now), updated_at: datetime($now)})
		CREATE (c:ChatConversation {id: $conv, name: 'Conversation ' + $conv,
		                           notebook_id: $notebook, user_id: $user,
		                           tenant_id: $tenant, space_id: $space,
		                           space_type: 'personal', message_count: 1,
		                           created_at: datetime($now), updated_at: datetime($now)})
		CREATE (m:ChatMessage {id: $conv + '-msg', conversation_id: $conv, tenant_id: $tenant,
		                       role: 'user', content: 'private message for ' + $tenant,
		                       is_error: false, metadata: '{}', created_at: datetime($now)})
		CREATE (cm:Comment {id: $comment, content: 'private comment for ' + $tenant,
		                    author_id: $user, resource_id: $notebook,
		                    resource_type: 'notebook', parent_id: '',
		                    tenant_id: $tenant, mentions: [], edited: false,
		                    created_at: datetime($now), updated_at: datetime($now)})
		CREATE (n)-[:BELONGS_TO]->(sp)
		CREATE (n)-[:OWNED_BY]->(u)
		CREATE (d)-[:BELONGS_TO]->(n)
		CREATE (d)-[:OWNED_BY]->(u)
		CREATE (c)-[:BELONGS_TO]->(n)
		CREATE (c)-[:CREATED_BY]->(u)
		CREATE (m)-[:PART_OF]->(c)
		CREATE (cm)-[:COMMENTED_ON]->(n)
		CREATE (cm)-[:AUTHORED_BY]->(u)
	`, params)
	if err != nil {
		t.Fatalf("seed tenant %s: %v", f.tenant, err)
	}
	return f
}

// Note on the deliberate collisions in the fixture: both tenants' documents
// share a url, storage_path and processing_job_id, and their filenames differ
// only by tenant. That is what makes the FindDocumentByURL and
// FindDocumentByAudiModalFileID cases below meaningful — before TEST-3 the
// filename fallback searched every tenant and returned whichever row was
// touched most recently.

func TestTenantIsolation_ReadsAcrossTenantsReturnNothing(t *testing.T) {
	env := setupIsolationEnv(t)
	ctx := context.Background()
	A, B := env.A, env.B

	t.Run("GetNotebookByID", func(t *testing.T) {
		if nb, err := env.notebooks.GetNotebookByID(ctx, B.notebookID, A.userID, A.spaceCtx); err == nil {
			t.Errorf("tenant A read tenant B's notebook: %+v", nb)
		}
	})

	t.Run("GetDocumentByID", func(t *testing.T) {
		if doc, err := env.docs.GetDocumentByID(ctx, B.documentID, A.userID, A.spaceCtx); err == nil {
			t.Errorf("tenant A read tenant B's document: %+v", doc)
		}
	})

	t.Run("GetConversation", func(t *testing.T) {
		if conv, err := env.convs.GetConversation(ctx, B.convID, A.userID, A.tenant); err == nil {
			t.Errorf("tenant A read tenant B's conversation: %+v", conv)
		}
	})

	// The one that matters most for the child-node rule: messages are reached
	// through their parent conversation, so a foreign conversation id must yield
	// an empty list rather than another tenant's chat history.
	t.Run("GetMessages", func(t *testing.T) {
		msgs, err := env.convs.GetMessages(ctx, B.convID, A.tenant, 50, 0)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if msgs.Total != 0 || len(msgs.Messages) != 0 {
			t.Errorf("tenant A read %d of tenant B's messages (total %d)", len(msgs.Messages), msgs.Total)
		}
	})

	t.Run("GetAllMessages", func(t *testing.T) {
		msgs, err := env.convs.GetAllMessages(ctx, B.convID, A.tenant)
		if err != nil {
			t.Fatalf("GetAllMessages: %v", err)
		}
		if len(msgs) != 0 {
			t.Errorf("tenant A read %d of tenant B's messages", len(msgs))
		}
	})

	t.Run("ListConversations", func(t *testing.T) {
		convs, err := env.convs.ListConversations(ctx, B.notebookID, A.tenant)
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		if convs.Total != 0 {
			t.Errorf("tenant A listed %d conversations in tenant B's notebook", convs.Total)
		}
	})

	t.Run("GetComments", func(t *testing.T) {
		got, err := env.comments.GetComments(ctx, B.notebookID, A.tenant, "")
		if err != nil {
			t.Fatalf("GetComments: %v", err)
		}
		if got.Total != 0 {
			t.Errorf("tenant A read %d of tenant B's comments", got.Total)
		}
	})

	// Both tenants' documents share this url and processing_job_id on purpose.
	t.Run("FindDocumentByURL", func(t *testing.T) {
		doc, err := env.docs.FindDocumentByURL(ctx, "https://example.test/shared-name.pdf", A.tenant)
		if err != nil {
			t.Fatalf("FindDocumentByURL: %v", err)
		}
		if doc != nil && doc.TenantID != A.tenant {
			t.Errorf("FindDocumentByURL returned tenant %q to tenant %q", doc.TenantID, A.tenant)
		}
	})

	t.Run("FindDocumentByAudiModalFileID", func(t *testing.T) {
		doc, err := env.docs.FindDocumentByAudiModalFileID(ctx, "shared-job-id", A.tenant)
		if err != nil {
			t.Fatalf("FindDocumentByAudiModalFileID: %v", err)
		}
		if doc != nil && doc.TenantID != A.tenant {
			t.Errorf("FindDocumentByAudiModalFileID returned tenant %q to tenant %q", doc.TenantID, A.tenant)
		}
	})
}

func TestTenantIsolation_WritesAcrossTenantsAreRejected(t *testing.T) {
	env := setupIsolationEnv(t)
	ctx := context.Background()
	A, B := env.A, env.B

	t.Run("UpdateConversation", func(t *testing.T) {
		_, err := env.convs.UpdateConversation(ctx, B.convID,
			models.UpdateConversationRequest{Name: "renamed by tenant A"}, B.userID, A.tenant)
		if err == nil {
			t.Error("tenant A renamed tenant B's conversation")
		}
		env.assertUnchanged(t, "ChatConversation", B.convID, "name", "Conversation "+B.convID)
	})

	t.Run("AddMessage", func(t *testing.T) {
		_, err := env.convs.AddMessage(ctx, B.convID, A.tenant, "user", "injected by tenant A", false, nil)
		if err == nil {
			t.Error("tenant A appended a message to tenant B's conversation")
		}
		env.assertMessageCount(t, B.convID, 1)
	})

	t.Run("UpdateComment", func(t *testing.T) {
		_, err := env.comments.UpdateComment(ctx, B.notebookID, B.commentID,
			models.UpdateCommentRequest{Content: "edited by tenant A"}, B.userID, A.tenant)
		if err == nil {
			t.Error("tenant A edited tenant B's comment")
		}
		env.assertUnchanged(t, "Comment", B.commentID, "content", "private comment for "+B.tenant)
	})

	t.Run("DeleteConversation", func(t *testing.T) {
		err := env.convs.DeleteConversation(ctx, B.convID, B.userID, A.spaceCtx)
		if err == nil {
			t.Error("tenant A deleted tenant B's conversation")
		}
		env.assertExists(t, "ChatConversation", B.convID)
	})

	t.Run("DeleteComment", func(t *testing.T) {
		err := env.comments.DeleteComment(ctx, B.notebookID, B.commentID, B.userID, A.spaceCtx)
		if err == nil {
			t.Error("tenant A deleted tenant B's comment")
		}
		env.assertExists(t, "Comment", B.commentID)
	})

	t.Run("ShareNotebook", func(t *testing.T) {
		err := env.notebooks.ShareNotebook(ctx, B.notebookID,
			models.NotebookShareRequest{UserIDs: []string{A.userID}, Permissions: []string{"write"}},
			A.userID, A.spaceCtx)
		if err == nil {
			t.Error("tenant A shared tenant B's notebook with itself")
		}
		env.assertNoShare(t, B.notebookID, A.userID)
	})
}

// The half that catches an over-tight filter. Every assertion here would also
// have passed before TEST-3; the point is that it still passes after.
func TestTenantIsolation_OwnTenantStillWorks(t *testing.T) {
	env := setupIsolationEnv(t)
	ctx := context.Background()
	A := env.A

	t.Run("GetNotebookByID", func(t *testing.T) {
		nb, err := env.notebooks.GetNotebookByID(ctx, A.notebookID, A.userID, A.spaceCtx)
		if err != nil {
			t.Fatalf("tenant A cannot read its own notebook: %v", err)
		}
		if nb.ID != A.notebookID {
			t.Errorf("got notebook %q, want %q", nb.ID, A.notebookID)
		}
	})

	t.Run("GetDocumentByID", func(t *testing.T) {
		doc, err := env.docs.GetDocumentByID(ctx, A.documentID, A.userID, A.spaceCtx)
		if err != nil {
			t.Fatalf("tenant A cannot read its own document: %v", err)
		}
		if doc.ID != A.documentID {
			t.Errorf("got document %q, want %q", doc.ID, A.documentID)
		}
	})

	t.Run("GetConversation", func(t *testing.T) {
		conv, err := env.convs.GetConversation(ctx, A.convID, A.userID, A.tenant)
		if err != nil {
			t.Fatalf("tenant A cannot read its own conversation: %v", err)
		}
		if conv.ID != A.convID {
			t.Errorf("got conversation %q, want %q", conv.ID, A.convID)
		}
	})

	// Guards the parent-traversal rewrite: reaching messages through the
	// conversation must still find them.
	t.Run("GetMessages", func(t *testing.T) {
		msgs, err := env.convs.GetMessages(ctx, A.convID, A.tenant, 50, 0)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if msgs.Total != 1 || len(msgs.Messages) != 1 {
			t.Fatalf("got %d messages (total %d), want 1 — the parent traversal is too tight",
				len(msgs.Messages), msgs.Total)
		}
		if want := "private message for " + A.tenant; msgs.Messages[0].Content != want {
			t.Errorf("got content %q, want %q", msgs.Messages[0].Content, want)
		}
	})

	t.Run("GetAllMessages", func(t *testing.T) {
		msgs, err := env.convs.GetAllMessages(ctx, A.convID, A.tenant)
		if err != nil {
			t.Fatalf("GetAllMessages: %v", err)
		}
		if len(msgs) != 1 {
			t.Errorf("got %d messages, want 1", len(msgs))
		}
	})

	t.Run("ListConversations", func(t *testing.T) {
		convs, err := env.convs.ListConversations(ctx, A.notebookID, A.tenant)
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		if convs.Total != 1 {
			t.Errorf("got %d conversations, want 1", convs.Total)
		}
	})

	t.Run("GetComments", func(t *testing.T) {
		got, err := env.comments.GetComments(ctx, A.notebookID, A.tenant, "")
		if err != nil {
			t.Fatalf("GetComments: %v", err)
		}
		if got.Total != 1 {
			t.Fatalf("got %d comments, want 1", got.Total)
		}
		if want := "private comment for " + A.tenant; got.Comments[0].Content != want {
			t.Errorf("got content %q, want %q", got.Comments[0].Content, want)
		}
	})

	t.Run("AddMessage", func(t *testing.T) {
		msg, err := env.convs.AddMessage(ctx, A.convID, A.tenant, "assistant", "reply", false, nil)
		if err != nil {
			t.Fatalf("tenant A cannot append to its own conversation: %v", err)
		}
		if msg.Content != "reply" {
			t.Errorf("got content %q, want %q", msg.Content, "reply")
		}
		// The new message must carry its tenant, so it is isolated on its own and
		// not only by traversal.
		env.assertNodeProperty(t, "ChatMessage", msg.ID, "tenant_id", A.tenant)
	})

	t.Run("FindDocumentByURL", func(t *testing.T) {
		doc, err := env.docs.FindDocumentByURL(ctx, "https://example.test/shared-name.pdf", A.tenant)
		if err != nil {
			t.Fatalf("FindDocumentByURL: %v", err)
		}
		if doc == nil {
			t.Fatal("tenant A cannot find its own document by URL")
		}
		if doc.TenantID != A.tenant {
			t.Errorf("got tenant %q, want %q", doc.TenantID, A.tenant)
		}
	})

	t.Run("UpdateConversation", func(t *testing.T) {
		conv, err := env.convs.UpdateConversation(ctx, A.convID,
			models.UpdateConversationRequest{Name: "renamed by owner"}, A.userID, A.tenant)
		if err != nil {
			t.Fatalf("tenant A cannot rename its own conversation: %v", err)
		}
		if conv.Name != "renamed by owner" {
			t.Errorf("got name %q, want %q", conv.Name, "renamed by owner")
		}
	})
}

// --- assertion helpers -------------------------------------------------------

// count runs a query whose single column "c" is a count, and returns it.
func (e *isolationEnv) count(t *testing.T, query string, params map[string]interface{}) int {
	t.Helper()
	res, err := e.neo4j.ExecuteQuery(context.Background(), query, params)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if len(res.Records) == 0 {
		return 0
	}
	v, ok := res.Records[0].Get("c")
	if !ok || v == nil {
		return 0
	}
	n, _ := v.(int64)
	return int(n)
}

func (e *isolationEnv) assertExists(t *testing.T, label, id string) {
	t.Helper()
	got := e.count(t, fmt.Sprintf("MATCH (n:%s {id: $id}) RETURN count(n) AS c", label),
		map[string]interface{}{"id": id})
	if got != 1 {
		t.Errorf("%s %s: found %d nodes, want 1 — a cross-tenant write took effect", label, id, got)
	}
}

func (e *isolationEnv) assertNodeProperty(t *testing.T, label, id, prop, want string) {
	t.Helper()
	res, err := e.neo4j.ExecuteQuery(context.Background(),
		fmt.Sprintf("MATCH (n:%s {id: $id}) RETURN n.%s AS v", label, prop),
		map[string]interface{}{"id": id})
	if err != nil {
		t.Fatalf("read %s.%s: %v", label, prop, err)
	}
	if len(res.Records) == 0 {
		t.Fatalf("%s %s not found", label, id)
	}
	v, _ := res.Records[0].Get("v")
	if got, _ := v.(string); got != want {
		t.Errorf("%s %s .%s = %q, want %q", label, id, prop, got, want)
	}
}

func (e *isolationEnv) assertUnchanged(t *testing.T, label, id, prop, want string) {
	t.Helper()
	e.assertNodeProperty(t, label, id, prop, want)
}

func (e *isolationEnv) assertMessageCount(t *testing.T, convID string, want int) {
	t.Helper()
	got := e.count(t, `
		MATCH (c:ChatConversation {id: $id})<-[:PART_OF]-(m:ChatMessage)
		RETURN count(m) AS c
	`, map[string]interface{}{"id": convID})
	if got != want {
		t.Errorf("conversation %s has %d messages, want %d", convID, got, want)
	}
}

func (e *isolationEnv) assertNoShare(t *testing.T, notebookID, userID string) {
	t.Helper()
	got := e.count(t, `
		MATCH (n:Notebook {id: $nb})-[r:SHARED_WITH]->(u:User {id: $u})
		RETURN count(r) AS c
	`, map[string]interface{}{"nb": notebookID, "u": userID})
	if got != 0 {
		t.Errorf("notebook %s is shared with %s (%d edges) — a cross-tenant share took effect",
			notebookID, userID, got)
	}
}

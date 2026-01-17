package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/redis/go-redis/v9"

	"github.com/Tributary-ai-services/aether-be/internal/config"
	"github.com/Tributary-ai-services/aether-be/internal/database"
	"github.com/Tributary-ai-services/aether-be/internal/logger"
	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/services"
)

func main() {
	fmt.Println("Setting up demo data...")

	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logger
	loggerInstance, err := logger.NewLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Setup Neo4j
	neo4jClient, err := database.NewNeo4jClient(cfg.Neo4j, loggerInstance)
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}
	defer neo4jClient.Close()

	// Setup Redis
	redisClient := database.NewRedisClient(&redis.Options{
		Addr:     cfg.Redis.Host + ":" + cfg.Redis.Port,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.Database,
	})

	ctx := context.Background()

	// Initialize services
	userService := services.NewUserService(neo4jClient, redisClient, loggerInstance)
	orgService := services.NewOrganizationService(neo4jClient, redisClient, loggerInstance)
	teamService := services.NewTeamService(neo4jClient, redisClient, loggerInstance)

	// Clear existing demo data if exists
	fmt.Println("Clearing existing demo data...")
	if err := clearDemoData(ctx, neo4jClient); err != nil {
		log.Printf("Warning: Failed to clear existing data: %v", err)
	}

	// Create demo users
	fmt.Println("Creating demo users...")
	users, err := createDemoUsers(ctx, userService)
	if err != nil {
		log.Fatalf("Failed to create demo users: %v", err)
	}

	// Create demo organizations
	fmt.Println("Creating demo organizations...")
	orgs, err := createDemoOrganizations(ctx, orgService, users)
	if err != nil {
		log.Fatalf("Failed to create demo organizations: %v", err)
	}

	// Create demo teams
	fmt.Println("Creating demo teams...")
	teams, err := createDemoTeams(ctx, teamService, users, orgs)
	if err != nil {
		log.Fatalf("Failed to create demo teams: %v", err)
	}

	// Setup team memberships
	fmt.Println("Setting up team memberships...")
	if err := setupTeamMemberships(ctx, teamService, users, teams); err != nil {
		log.Fatalf("Failed to setup team memberships: %v", err)
	}

	fmt.Println("Demo data setup complete!")
	fmt.Printf("Created %d users, %d organizations, %d teams\n", len(users), len(orgs), len(teams))
	
	// Print login info
	fmt.Println("\nDemo login credentials:")
	for i, user := range users {
		fmt.Printf("%d. %s (%s) - Keycloak ID: %s\n", i+1, user.FullName, user.Email, user.KeycloakID)
	}
}

func clearDemoData(ctx context.Context, neo4jClient *database.Neo4jClient) error {
	session := neo4jClient.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	queries := []string{
		"MATCH ()-[r:MEMBER_OF]->() DELETE r",
		"MATCH (t:Team) DELETE t",
		"MATCH (o:Organization) DELETE o",
		"MATCH (u:User) WHERE u.email ENDS WITH '@demo.aether' DELETE u",
	}

	for _, query := range queries {
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			result, err := tx.Run(ctx, query, nil)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func createDemoUsers(ctx context.Context, userService *services.UserService) ([]*models.User, error) {
	demoUsers := []models.UserCreateRequest{
		{
			KeycloakID: "demo-john-doe",
			Email:      "john@demo.aether",
			Username:   "john.doe",
			FullName:   "John Doe",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-jane-smith",
			Email:      "jane@demo.aether",
			Username:   "jane.smith",
			FullName:   "Jane Smith",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-bob-wilson",
			Email:      "bob@demo.aether",
			Username:   "bob.wilson",
			FullName:   "Bob Wilson",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-alice-brown",
			Email:      "alice@demo.aether",
			Username:   "alice.brown",
			FullName:   "Alice Brown",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-charlie-davis",
			Email:      "charlie@demo.aether",
			Username:   "charlie.davis",
			FullName:   "Charlie Davis",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-david-lee",
			Email:      "david@demo.aether",
			Username:   "david.lee",
			FullName:   "David Lee",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-sarah-johnson",
			Email:      "sarah@demo.aether",
			Username:   "sarah.johnson",
			FullName:   "Sarah Johnson",
			AvatarURL:  "",
		},
		{
			KeycloakID: "demo-maria-garcia",
			Email:      "maria@demo.aether",
			Username:   "maria.garcia",
			FullName:   "Maria Garcia",
			AvatarURL:  "",
		},
	}

	users := make([]*models.User, len(demoUsers))
	for i, req := range demoUsers {
		user, err := userService.CreateUser(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create user %s: %w", req.Email, err)
		}
		users[i] = user
	}

	return users, nil
}

func createDemoOrganizations(ctx context.Context, orgService *services.OrganizationService, users []*models.User) ([]*models.Organization, error) {
	// Find specific users for organization ownership
	johnDoe := findUserByEmail(users, "john@demo.aether")
	mariaGarcia := findUserByEmail(users, "maria@demo.aether")

	demoOrgs := []struct {
		req     models.OrganizationCreateRequest
		creator *models.User
	}{
		{
			req: models.OrganizationCreateRequest{
				Name:         "Acme Corporation",
				Slug:         "acme-corp",
				Description:  "Leading provider of AI-powered enterprise solutions",
				Website:      "https://acme.com",
				Location:     "San Francisco, CA",
				Visibility:   "public",
				BillingEmail: "billing@demo.aether",
				Billing: map[string]interface{}{
					"plan":         "enterprise",
					"seats":        500,
					"billingEmail": "billing@demo.aether",
				},
				Settings: map[string]interface{}{
					"membersCanCreateRepositories": true,
					"membersCanCreateTeams":        true,
					"membersCanFork":               true,
					"defaultMemberPermissions":     "read",
					"twoFactorRequired":            true,
				},
			},
			creator: johnDoe,
		},
		{
			req: models.OrganizationCreateRequest{
				Name:         "DataTech Labs",
				Slug:         "datatech-labs",
				Description:  "Research and development in machine learning",
				Website:      "https://datatech.io",
				Location:     "Austin, TX",
				Visibility:   "private",
				BillingEmail: "admin@demo.aether",
				Billing: map[string]interface{}{
					"plan":         "pro",
					"seats":        25,
					"billingEmail": "admin@demo.aether",
				},
				Settings: map[string]interface{}{
					"membersCanCreateRepositories": false,
					"membersCanCreateTeams":        false,
					"membersCanFork":               true,
					"defaultMemberPermissions":     "read",
					"twoFactorRequired":            false,
				},
			},
			creator: mariaGarcia,
		},
	}

	orgs := make([]*models.Organization, len(demoOrgs))
	for i, demo := range demoOrgs {
		org, err := orgService.CreateOrganization(ctx, demo.req, demo.creator.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create organization %s: %w", demo.req.Name, err)
		}
		orgs[i] = org
	}

	// Add additional members to organizations
	acmeCorp := orgs[0]
	datatechLabs := orgs[1]

	// Add members to Acme Corporation
	janeSmith := findUserByEmail(users, "jane@demo.aether")
	bobWilson := findUserByEmail(users, "bob@demo.aether")
	aliceBrown := findUserByEmail(users, "alice@demo.aether")

	acmeMembers := []struct {
		user       *models.User
		role       string
		title      string
		department string
	}{
		{janeSmith, "admin", "CTO", "Engineering"},
		{bobWilson, "member", "Senior Engineer", "Engineering"},
		{aliceBrown, "member", "Product Manager", "Product"},
	}

	for _, member := range acmeMembers {
		_, err := orgService.InviteOrganizationMember(ctx, acmeCorp.ID, models.OrganizationInviteRequest{
			Email:      member.user.Email,
			Role:       member.role,
			Title:      member.title,
			Department: member.department,
		}, johnDoe.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to add member %s to Acme Corp: %w", member.user.Email, err)
		}
	}

	// Add members to DataTech Labs
	davidLee := findUserByEmail(users, "david@demo.aether")

	_, err := orgService.InviteOrganizationMember(ctx, datatechLabs.ID, models.OrganizationInviteRequest{
		Email:      johnDoe.Email,
		Role:       "admin",
		Title:      "Advisor",
		Department: "Advisory",
	}, mariaGarcia.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to add John Doe to DataTech Labs: %w", err)
	}

	_, err = orgService.InviteOrganizationMember(ctx, datatechLabs.ID, models.OrganizationInviteRequest{
		Email:      davidLee.Email,
		Role:       "member",
		Title:      "ML Engineer",
		Department: "Research",
	}, mariaGarcia.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to add David Lee to DataTech Labs: %w", err)
	}

	return orgs, nil
}

func createDemoTeams(ctx context.Context, teamService *services.TeamService, users []*models.User, orgs []*models.Organization) ([]*models.Team, error) {
	acmeCorp := findOrgBySlug(orgs, "acme-corp")
	datatechLabs := findOrgBySlug(orgs, "datatech-labs")

	johnDoe := findUserByEmail(users, "john@demo.aether")
	janeSmith := findUserByEmail(users, "jane@demo.aether")
	mariaGarcia := findUserByEmail(users, "maria@demo.aether")

	demoTeams := []struct {
		req     models.TeamCreateRequest
		creator *models.User
	}{
		{
			req: models.TeamCreateRequest{
				Name:           "Engineering Team",
				Description:    "Core engineering and development team",
				OrganizationID: acmeCorp.ID,
				Visibility:     "private",
				Settings: map[string]interface{}{
					"allowExternalSharing":         false,
					"requireApprovalForJoining":    true,
					"defaultNotebookVisibility":    "team",
					"allowMemberInvites":           false,
					"allowMemberNotebookCreation":  true,
					"notificationsEnabled":         true,
				},
			},
			creator: johnDoe,
		},
		{
			req: models.TeamCreateRequest{
				Name:           "Data Science",
				Description:    "ML and data analysis team",
				OrganizationID: acmeCorp.ID,
				Visibility:     "organization",
				Settings: map[string]interface{}{
					"allowExternalSharing":         true,
					"requireApprovalForJoining":    false,
					"defaultNotebookVisibility":    "organization",
					"allowMemberInvites":           true,
					"allowMemberNotebookCreation":  true,
					"notificationsEnabled":         true,
				},
			},
			creator: janeSmith,
		},
		{
			req: models.TeamCreateRequest{
				Name:           "Research Team",
				Description:    "Research and development initiatives",
				OrganizationID: acmeCorp.ID,
				Visibility:     "private",
				Settings: map[string]interface{}{
					"allowExternalSharing":         false,
					"requireApprovalForJoining":    true,
					"defaultNotebookVisibility":    "team",
					"allowMemberInvites":           false,
					"allowMemberNotebookCreation":  true,
					"notificationsEnabled":         true,
				},
			},
			creator: janeSmith,
		},
		{
			req: models.TeamCreateRequest{
				Name:           "ML Research",
				Description:    "Advanced machine learning research",
				OrganizationID: datatechLabs.ID,
				Visibility:     "private",
				Settings: map[string]interface{}{
					"allowExternalSharing":         false,
					"requireApprovalForJoining":    true,
					"defaultNotebookVisibility":    "team",
					"allowMemberInvites":           false,
					"allowMemberNotebookCreation":  true,
					"notificationsEnabled":         true,
				},
			},
			creator: mariaGarcia,
		},
	}

	teams := make([]*models.Team, len(demoTeams))
	for i, demo := range demoTeams {
		team, err := teamService.CreateTeam(ctx, demo.req, demo.creator.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create team %s: %w", demo.req.Name, err)
		}
		teams[i] = team
	}

	return teams, nil
}

func setupTeamMemberships(ctx context.Context, teamService *services.TeamService, users []*models.User, teams []*models.Team) error {
	// Find users
	johnDoe := findUserByEmail(users, "john@demo.aether")
	janeSmith := findUserByEmail(users, "jane@demo.aether")
	bobWilson := findUserByEmail(users, "bob@demo.aether")
	aliceBrown := findUserByEmail(users, "alice@demo.aether")
	charlieDavis := findUserByEmail(users, "charlie@demo.aether")
	davidLee := findUserByEmail(users, "david@demo.aether")
	sarahJohnson := findUserByEmail(users, "sarah@demo.aether")

	// Find teams
	engTeam := findTeamByName(teams, "Engineering Team")
	dataTeam := findTeamByName(teams, "Data Science")
	researchTeam := findTeamByName(teams, "Research Team")
	mlTeam := findTeamByName(teams, "ML Research")

	// Engineering Team members
	engMembers := []struct {
		user *models.User
		role string
	}{
		{janeSmith, "admin"},
		{bobWilson, "member"},
		{aliceBrown, "member"},
		{charlieDavis, "viewer"},
	}

	for _, member := range engMembers {
		_, err := teamService.InviteTeamMember(ctx, engTeam.ID, models.TeamInviteRequest{
			Email: member.user.Email,
			Role:  member.role,
		}, johnDoe.ID)
		if err != nil {
			return fmt.Errorf("failed to add %s to Engineering Team: %w", member.user.Email, err)
		}
	}

	// Data Science Team members
	dataMembers := []struct {
		user *models.User
		role string
	}{
		{johnDoe, "admin"},
		{davidLee, "member"},
	}

	for _, member := range dataMembers {
		_, err := teamService.InviteTeamMember(ctx, dataTeam.ID, models.TeamInviteRequest{
			Email: member.user.Email,
			Role:  member.role,
		}, janeSmith.ID)
		if err != nil {
			return fmt.Errorf("failed to add %s to Data Science Team: %w", member.user.Email, err)
		}
	}

	// Research Team members
	_, err := teamService.InviteTeamMember(ctx, researchTeam.ID, models.TeamInviteRequest{
		Email: bobWilson.Email,
		Role:  "member",
	}, janeSmith.ID)
	if err != nil {
		return fmt.Errorf("failed to add Bob to Research Team: %w", err)
	}

	_, err = teamService.InviteTeamMember(ctx, researchTeam.ID, models.TeamInviteRequest{
		Email: sarahJohnson.Email,
		Role:  "viewer",
	}, janeSmith.ID)
	if err != nil {
		return fmt.Errorf("failed to add Sarah to Research Team: %w", err)
	}

	// ML Research Team members (DataTech Labs)
	_, err = teamService.InviteTeamMember(ctx, mlTeam.ID, models.TeamInviteRequest{
		Email: davidLee.Email,
		Role:  "admin",
	}, johnDoe.ID)
	if err != nil {
		return fmt.Errorf("failed to add David to ML Research Team: %w", err)
	}

	return nil
}

// Helper functions
func findUserByEmail(users []*models.User, email string) *models.User {
	for _, user := range users {
		if user.Email == email {
			return user
		}
	}
	return nil
}

func findOrgBySlug(orgs []*models.Organization, slug string) *models.Organization {
	for _, org := range orgs {
		if org.Slug == slug {
			return org
		}
	}
	return nil
}

func findTeamByName(teams []*models.Team, name string) *models.Team {
	for _, team := range teams {
		if team.Name == name {
			return team
		}
	}
	return nil
}
// Migration: remove test residue and orphaned tenants (TEST-3)
//
// Purpose
// -------
// Clear the leftovers that accumulated while the space model was being built, so
// the graph holds only tenants with a live owner. This is hygiene. Nothing in
// the TEST-3 code fix depends on it, and that is deliberate — see "Why the
// child nodes are optional" below before deciding to run this.
//
// THIS MIGRATION DELETES PRODUCTION DATA AND CANNOT BE UNDONE.
// Confirm the nightly tas-backup CronJob's most recent run completed and wrote
// a neo4j.cypher.gz before running it:
//
//   kubectl get jobs -n tas-shared | grep tas-backup
//   kubectl logs -n tas-shared job/<latest> | grep neo4j.cypher.gz
//
// Measured against production 2026-09-30. Re-run the dry-run block below and
// compare before applying; if the numbers have moved, work out why first. This
// is the discipline migration 006 followed, where the dry run matched the apply
// exactly (3 backfilled, 7 cleared).
//
// Expected effect, as measured:
//
//   Step 1  4 test users + 15 nodes in their tenants
//           (6 Document, 5 Notebook, 4 Space)
//   Step 2  10 nodes across 6 tenants with no owning user
//           tenant_1768924118                            1 Space
//           tenant_1768953334                            1 Space
//           tenant_1769019019                            1 Space, 1 Notebook, 1 Document
//           tenant_1772236887                            1 Space, 4 Database
//           tenant_8014617b-4                            1 Agent
//           tenant_87f5d6f6-116a-4460-926e-f0b281966d3d  1 Organization
//   Step 3  46 child nodes with no tenant_id
//           (24 ChatMessage, 12 WorkflowStep, 6 WorkflowVersion, 4 WorkflowTrigger)
//
// Preserved: john@scharber.com (tenant_1766596584, 160 nodes) and
// candace@scharber.com (tenant_a97beaea-064e-4cf4-9deb-ab6a6e7195e4, 5 nodes).
// Keep both. The tenant-isolation tests want two real tenants, and one tenant
// cannot demonstrate isolation from anything.
//
// Step 2 deletes a whole orphan tenant, not just its Space. An earlier draft
// matched only nodes whose tenant had no Space, which would have deleted the
// orphan Space in tenant_1769019019 and left its Notebook and Document behind —
// turning a tidy orphan into a worse one. If you narrow this predicate, check
// what it leaves rather than what it removes.
//
// Why the child nodes in step 3 are optional
// ------------------------------------------
// ChatMessage, WorkflowStep, WorkflowVersion and WorkflowTrigger have never
// carried a tenant_id — 0 of 46 in production. That is now harmless on both
// sides: reads reach them only through their tenant-filtered parent
// (conversation.go GetMessages, GetAllMessages), and writes stamp tenant_id
// from the parent (AddMessage, CreateWorkflow, createVersionSnapshot). So the
// legacy rows are correctly isolated as they stand and need no backfill.
//
// Step 3 is therefore tidiness, not a fix. It makes the property universal so
// the isolation does not rest on traversal alone. Skipping it costs nothing;
// running it destroys 24 real chat messages in john's tenant. Decide
// accordingly — and note that this is the step with user-visible content in it.

// =============================================================================
// DRY RUN - run this block first and compare against the numbers above.
// It only counts; it changes nothing.
// =============================================================================

// :param dryRun => true

// --- Step 1: test accounts and everything in their tenants -------------------
MATCH (u:User)
WHERE u.email =~ 'test-user-.*@example\\.com'
   OR u.email =~ 'testuser.*@test\\.com'
   OR u.email =~ 'verify-.*@scharber\\.com'
WITH collect(u) AS testUsers, collect(u.personal_tenant_id) AS testTenants
OPTIONAL MATCH (n) WHERE n.tenant_id IN testTenants
RETURN 'step1' AS step,
       size(testUsers) AS users,
       count(n) AS tenantNodes,
       testTenants AS tenants;

// --- Step 2: tenants with no owning user ------------------------------------
MATCH (u:User) WITH collect(u.personal_tenant_id) AS liveTenants
MATCH (n)
WHERE n.tenant_id IS NOT NULL AND NOT n.tenant_id IN liveTenants
RETURN 'step2' AS step, n.tenant_id AS orphanTenant, labels(n)[0] AS label, count(*) AS nodes
ORDER BY orphanTenant, label;

// --- Step 3: child nodes with no tenant_id ----------------------------------
MATCH (n)
WHERE n.tenant_id IS NULL
  AND any(l IN labels(n) WHERE l IN ['ChatMessage', 'WorkflowStep', 'WorkflowVersion', 'WorkflowTrigger'])
RETURN 'step3' AS step, labels(n)[0] AS label, count(*) AS nodes
ORDER BY label;

// =============================================================================
// APPLY - only after the dry run matches.
// Run the steps one at a time and check the counters between them.
// =============================================================================

// --- Step 1: delete test accounts and their tenants' data -------------------
// Order matters: collect the tenant ids before deleting the users that name
// them, or the tenants become unfindable halfway through.
MATCH (u:User)
WHERE u.email =~ 'test-user-.*@example\\.com'
   OR u.email =~ 'testuser.*@test\\.com'
   OR u.email =~ 'verify-.*@scharber\\.com'
WITH collect(u.personal_tenant_id) AS testTenants
MATCH (n) WHERE n.tenant_id IN testTenants
DETACH DELETE n;

MATCH (u:User)
WHERE u.email =~ 'test-user-.*@example\\.com'
   OR u.email =~ 'testuser.*@test\\.com'
   OR u.email =~ 'verify-.*@scharber\\.com'
DETACH DELETE u;

// --- Step 2: delete tenants with no owning user -----------------------------
MATCH (u:User) WITH collect(u.personal_tenant_id) AS liveTenants
MATCH (n)
WHERE n.tenant_id IS NOT NULL AND NOT n.tenant_id IN liveTenants
DETACH DELETE n;

// --- Step 3: delete child nodes with no tenant_id ---------------------------
// OPTIONAL, and the only step that destroys content a user might miss: 24 of
// these ChatMessages are real messages in john's tenant. Read the note above.
// MATCH (n)
// WHERE n.tenant_id IS NULL
//   AND any(l IN labels(n) WHERE l IN ['ChatMessage', 'WorkflowStep', 'WorkflowVersion', 'WorkflowTrigger'])
// DETACH DELETE n;

// =============================================================================
// VERIFY - after applying
// =============================================================================

// Every remaining tenant should have an owning user.
MATCH (u:User) WITH collect(u.personal_tenant_id) AS liveTenants
MATCH (n) WHERE n.tenant_id IS NOT NULL AND NOT n.tenant_id IN liveTenants
RETURN 'orphan tenants remaining (want 0)' AS check, count(*) AS nodes;

// Both surviving users and their node counts.
MATCH (u:User)
OPTIONAL MATCH (n) WHERE n.tenant_id = u.personal_tenant_id
RETURN u.email AS email, u.personal_tenant_id AS tenant, count(n) AS nodes
ORDER BY nodes DESC;

// Tenant coverage per label: with step 3 skipped, the four child labels are
// still expected to show 0 covered, and that is fine — their parents carry it.
MATCH (n)
WITH labels(n)[0] AS label, count(*) AS total,
     sum(CASE WHEN n.tenant_id IS NULL THEN 0 ELSE 1 END) AS withTenant
RETURN label, total, withTenant
ORDER BY total DESC;

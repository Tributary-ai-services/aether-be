// Migration: Add tutorial_completed fields to existing users
// Run this after deploying the updated backend

// Mark all existing users as having completed the tutorial
// This prevents existing users from seeing the tutorial again
MATCH (u:User)
WHERE u.tutorial_completed IS NULL
SET u.tutorial_completed = true,
    u.tutorial_completed_at = datetime()
RETURN count(u) as updated_users;

// Verify the migration
MATCH (u:User)
RETURN
  count(u) as total_users,
  count(CASE WHEN u.tutorial_completed = true THEN 1 END) as completed_tutorials,
  count(CASE WHEN u.tutorial_completed = false THEN 1 END) as pending_tutorials;

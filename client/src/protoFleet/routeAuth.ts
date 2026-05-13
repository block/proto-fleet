// Auth configuration — which routes require authentication. Lives in
// a router-independent module so App.tsx can import it without
// creating a cycle through router.tsx.

export const requiresAuth: Record<string, boolean> = {
  "/auth": false,
  "/welcome": false,
  "/update-password": true, // Requires auth but is a special intermediate step
  "/fleet-down": false, // Error page doesn't require auth
  // All other routes require auth by default
};

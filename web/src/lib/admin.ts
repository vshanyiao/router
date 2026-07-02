import { auth } from './auth'
import { prisma } from './db'

/** Thrown by requireAdmin; routes catch it to return the right HTTP status. */
export class AdminAuthError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

/**
 * Assert the caller is an authenticated admin. Returns the admin's id + email
 * on success. Throws AdminAuthError (401 if unauthenticated, 403 if not admin)
 * — API routes should catch and map to NextResponse.
 */
export async function requireAdmin(): Promise<{ id: string; email: string }> {
  const session = await auth()
  if (!session?.user?.id) throw new AdminAuthError(401, 'Unauthorized')
  const user = await prisma.user.findUnique({
    where: { id: session.user.id },
    select: { id: true, email: true, isAdmin: true },
  })
  if (!user?.isAdmin) throw new AdminAuthError(403, 'Forbidden')
  return { id: user.id, email: user.email }
}

/** Write an audit_logs row for an admin mutation. Never throws to the caller. */
export async function logAdminAction(
  actorId: string,
  kind: string,
  payload: unknown,
  targetUserId?: string,
): Promise<void> {
  try {
    await prisma.auditLog.create({
      data: {
        userId: actorId,
        targetUserId: targetUserId ?? null,
        kind,
        payload: payload as object,
      },
    })
  } catch (e) {
    console.error('logAdminAction failed:', kind, e)
  }
}

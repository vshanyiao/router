import { NextRequest, NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

// GET /api/topup/status?pi=<uuid>
// Returns the current status of a single payment intent, scoped to the caller.
// The success page polls this after Stripe redirect: status 'succeeded' is an
// unambiguous "credits landed" signal (vs. guessing from balance deltas).
export async function GET(req: NextRequest) {
  const session = await auth()
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const piId = req.nextUrl.searchParams.get('pi')
  if (!piId) {
    return NextResponse.json({ error: 'Missing pi query param' }, { status: 400 })
  }

  const pi = await prisma.paymentIntent.findUnique({
    where: { id: piId },
    select: { userId: true, status: true, creditsAddedCents: true },
  })
  if (!pi || pi.userId !== session.user.id) {
    return NextResponse.json({ error: 'Not found' }, { status: 404 })
  }

  return NextResponse.json({
    status: pi.status,
    creditsAddedCents: Number(pi.creditsAddedCents),
  })
}

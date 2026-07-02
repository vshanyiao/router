import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const intents = await prisma.paymentIntent.findMany({
    where: { userId: session.user.id },
    orderBy: { createdAt: 'desc' },
    take: 20,
    select: {
      id: true,
      amountCents: true,
      creditsAddedCents: true,
      currency: true,
      status: true,
      createdAt: true,
      completedAt: true,
    },
  })

  // BigInt → Number for JSON serialization. amounts in cents fit safely
  // within Number.MAX_SAFE_INTEGER (~$90 trillion).
  return NextResponse.json({
    intents: intents.map((i) => ({
      ...i,
      amountCents: Number(i.amountCents),
      creditsAddedCents: Number(i.creditsAddedCents),
    })),
  })
}

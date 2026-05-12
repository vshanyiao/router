import NextAuth from 'next-auth'
import GitHub from 'next-auth/providers/github'
import Credentials from 'next-auth/providers/credentials'
import { PrismaAdapter } from '@auth/prisma-adapter'
import bcrypt from 'bcryptjs'
import { prisma } from './db'
import { env } from './env'

const providers = []

if (env.GITHUB_CLIENT_ID && env.GITHUB_CLIENT_SECRET) {
  providers.push(GitHub({
    clientId: env.GITHUB_CLIENT_ID,
    clientSecret: env.GITHUB_CLIENT_SECRET,
  }))
}

providers.push(Credentials({
  credentials: {
    email: { label: 'Email', type: 'email' },
    password: { label: 'Password', type: 'password' },
  },
  async authorize(credentials) {
    const email = credentials?.email as string
    const password = credentials?.password as string
    if (!email || !password) return null

    const user = await prisma.user.findUnique({ where: { email } })
    if (!user?.passwordHash) return null
    const ok = await bcrypt.compare(password, user.passwordHash)
    if (!ok) return null
    if (user.status !== 'active') return null
    return { id: user.id, email: user.email, name: user.email }
  },
}))

export const { handlers, signIn, signOut, auth } = NextAuth({
  adapter: PrismaAdapter(prisma) as any,
  // NextAuth v5 requires JWT session strategy when the Credentials provider
  // is in use. We trade the instant-revocation property of DB sessions for
  // Credentials compatibility; the proxy still picks up admin suspensions
  // within 30s via the Redis auth cache TTL (see proxy/internal/auth/auth.go).
  session: { strategy: 'jwt' },
  pages: { signIn: '/login' },
  providers,
  callbacks: {
    async signIn({ user, account, profile }) {
      if (account?.provider === 'github' && user.email) {
        const existing = await prisma.user.findUnique({ where: { email: user.email } })
        if (existing && !existing.githubId && profile?.id) {
          await prisma.user.update({
            where: { id: existing.id },
            data: {
              githubId: String(profile.id),
              emailVerifiedAt: existing.emailVerifiedAt ?? new Date(),
            },
          })
        }
      }
      return true
    },
    // JWT strategy doesn't auto-populate session.user.id. We persist the
    // database user id into the token on sign-in, then surface it on the
    // session object so server-side handlers can do `session.user.id`.
    async jwt({ token, user }) {
      if (user?.id) token.sub = user.id
      return token
    },
    async session({ session, token }) {
      if (session.user && token.sub) {
        (session.user as { id?: string }).id = token.sub
      }
      return session
    },
  },
  events: {
    async createUser({ user }) {
      if (user.id) {
        await prisma.user.update({
          where: { id: user.id },
          data: { emailVerifiedAt: new Date() },
        })
      }
    },
  },
})

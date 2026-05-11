import { Resend } from 'resend'
import { env } from './env'

const resend = env.RESEND_API_KEY ? new Resend(env.RESEND_API_KEY) : null

export async function sendVerificationEmail(to: string, verifyUrl: string): Promise<void> {
  if (!resend || !env.EMAIL_FROM) {
    // Dev fallback: print the link to stdout so the developer can verify
    console.log(`\n[DEV] Verification email to ${to}:\n  ${verifyUrl}\n`)
    return
  }

  const { error } = await resend.emails.send({
    from: env.EMAIL_FROM,
    to,
    subject: 'Verify your MaaS Router email',
    html: `
      <p>Welcome to MaaS Router.</p>
      <p>Click the link below to verify your email and get your $1 free trial credit:</p>
      <p><a href="${verifyUrl}">${verifyUrl}</a></p>
      <p>This link expires in 24 hours.</p>
    `,
  })
  if (error) throw new Error(`Failed to send verification email: ${error.message}`)
}

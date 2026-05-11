import { auth } from '@/lib/auth'

export default auth((req) => {
  if (!req.auth && req.nextUrl.pathname.startsWith('/dashboard')) {
    const loginUrl = new URL('/login', req.url)
    return Response.redirect(loginUrl)
  }
})

export const config = {
  matcher: ['/dashboard/:path*'],
}

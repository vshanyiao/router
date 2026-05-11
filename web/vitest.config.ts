import { defineConfig } from 'vitest/config'
import path from 'path'

export default defineConfig({
  test: {
    environment: 'node',
    env: {
      DATABASE_URL: 'postgres://app:dev@localhost:5432/maas_test',
      REDIS_URL: 'redis://localhost:6379',
      NEXTAUTH_URL: 'http://localhost:3000',
      NEXTAUTH_SECRET: 'test-secret-padded-to-at-least-32-bytes-please',
      HMAC_SERVER_SECRET: 'test-hmac-secret-padded-to-at-least-32-bytes',
    },
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
})

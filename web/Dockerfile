FROM node:22-alpine

WORKDIR /app

# pnpm 11 requires Node 22+. Pinning the version avoids surprises when
# corepack pulls a newer version that breaks on this base image.
RUN corepack enable && corepack prepare pnpm@11.1.0 --activate

COPY package.json pnpm-lock.yaml* pnpm-workspace.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install

COPY . .

EXPOSE 3000

CMD ["pnpm", "dev"]

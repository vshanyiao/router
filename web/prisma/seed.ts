import { PrismaClient } from '@prisma/client'

const prisma = new PrismaClient()

async function main() {
  // app_config defaults
  await prisma.appConfig.upsert({
    where: { key: 'default_markup_pct' },
    update: {},
    create: { key: 'default_markup_pct', value: 18 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'topup_presets_cents' },
    update: {},
    create: { key: 'topup_presets_cents', value: [500, 1000, 2000, 5000, 10000] },
  })
  await prisma.appConfig.upsert({
    where: { key: 'trial_credit_cents' },
    update: {},
    create: { key: 'trial_credit_cents', value: 100 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'cny_per_usd_rate' },
    update: {},
    create: { key: 'cny_per_usd_rate', value: 7.20 },
  })
  await prisma.appConfig.upsert({
    where: { key: 'rate_limit_per_user_per_minute' },
    update: {},
    create: { key: 'rate_limit_per_user_per_minute', value: 60 },
  })

  // Phase 0: seed exactly one model — GPT-4o
  await prisma.modelCatalog.upsert({
    where: { alias: 'openai/gpt-4o' },
    update: {},
    create: {
      alias: 'openai/gpt-4o',
      displayName: 'GPT-4o',
      upstreamProvider: 'openai',
      upstreamModelId: 'gpt-4o',
      contextWindow: 128000,
      supportsStreaming: true,
      supportsTools: true,
      supportsVision: true,
      inputCentsPerMillionTokens: 250,
      outputCentsPerMillionTokens: 1000,
      markupPct: 18,
      status: 'active',
      tags: ['frontier', 'vision', 'tools'],
      descriptionZh: 'OpenAI 最新前沿模型, 128K 上下文, 支持视觉和工具调用',
      descriptionEn: 'OpenAI flagship model, 128K context, supports vision and tools',
    },
  })

  console.log('Seed complete.')
}

main()
  .catch((e) => { console.error(e); process.exit(1) })
  .finally(async () => { await prisma.$disconnect() })

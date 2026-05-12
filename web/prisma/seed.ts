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

  const additionalModels = [
    {
      alias: 'openai/gpt-4o-mini',
      displayName: 'GPT-4o mini',
      upstreamProvider: 'openai',
      upstreamModelId: 'gpt-4o-mini',
      contextWindow: 128000,
      inputCentsPerMillionTokens: 15,
      outputCentsPerMillionTokens: 60,
      tags: ['cheap', 'fast'],
      descriptionEn: 'Cheap, fast OpenAI model for routine tasks',
      descriptionZh: '便宜快速的 OpenAI 小模型, 适合日常任务',
    },
    {
      alias: 'openai/o1',
      displayName: 'o1',
      upstreamProvider: 'openai',
      upstreamModelId: 'o1',
      contextWindow: 200000,
      supportsTools: false,
      inputCentsPerMillionTokens: 1500,
      outputCentsPerMillionTokens: 6000,
      tags: ['reasoning', 'frontier'],
      descriptionEn: 'Advanced reasoning model',
      descriptionZh: '高级推理模型',
    },
    {
      alias: 'anthropic/claude-sonnet-4-6',
      displayName: 'Claude 4.6 Sonnet',
      upstreamProvider: 'anthropic',
      upstreamModelId: 'claude-sonnet-4-6',
      contextWindow: 200000,
      inputCentsPerMillionTokens: 300,
      outputCentsPerMillionTokens: 1500,
      tags: ['frontier', 'vision', 'tools'],
      descriptionEn: 'Anthropic flagship model, strong coding + reasoning',
      descriptionZh: 'Anthropic 旗舰模型, 编码和推理能力出色',
    },
    {
      alias: 'anthropic/claude-haiku-4-5',
      displayName: 'Claude Haiku 4.5',
      upstreamProvider: 'anthropic',
      upstreamModelId: 'claude-haiku-4-5',
      contextWindow: 200000,
      inputCentsPerMillionTokens: 80,
      outputCentsPerMillionTokens: 400,
      tags: ['cheap', 'fast'],
      descriptionEn: 'Anthropic cheap/fast model',
      descriptionZh: 'Anthropic 便宜快速模型',
    },
    {
      alias: 'google/gemini-2.5-pro',
      displayName: 'Gemini 2.5 Pro',
      upstreamProvider: 'google',
      upstreamModelId: 'gemini-2.5-pro',
      contextWindow: 1000000,
      inputCentsPerMillionTokens: 125,
      outputCentsPerMillionTokens: 500,
      tags: ['frontier', 'vision', 'long-context'],
      descriptionEn: 'Google Gemini 2.5 Pro, 1M context',
      descriptionZh: 'Google Gemini 2.5 Pro, 一百万 token 上下文',
    },
    {
      alias: 'google/gemini-2.5-flash',
      displayName: 'Gemini 2.5 Flash',
      upstreamProvider: 'google',
      upstreamModelId: 'gemini-2.5-flash',
      contextWindow: 1000000,
      inputCentsPerMillionTokens: 8,
      outputCentsPerMillionTokens: 30,
      tags: ['cheap', 'fast', 'long-context'],
      descriptionEn: 'Google Gemini 2.5 Flash, 1M context, very cheap',
      descriptionZh: 'Google Gemini 2.5 Flash, 一百万 token 上下文, 非常便宜',
    },
  ]

  for (const m of additionalModels) {
    await prisma.modelCatalog.upsert({
      where: { alias: m.alias },
      update: {},
      create: {
        alias: m.alias,
        displayName: m.displayName,
        upstreamProvider: m.upstreamProvider,
        upstreamModelId: m.upstreamModelId,
        contextWindow: m.contextWindow,
        supportsStreaming: true,
        supportsTools: m.supportsTools ?? true,
        supportsVision: true,
        inputCentsPerMillionTokens: m.inputCentsPerMillionTokens,
        outputCentsPerMillionTokens: m.outputCentsPerMillionTokens,
        markupPct: 18,
        status: 'active',
        tags: m.tags,
        descriptionEn: m.descriptionEn,
        descriptionZh: m.descriptionZh,
      },
    })
  }

  console.log('Seed complete.')
}

main()
  .catch((e) => { console.error(e); process.exit(1) })
  .finally(async () => { await prisma.$disconnect() })

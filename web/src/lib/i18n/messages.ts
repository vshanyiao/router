// Lightweight bilingual dictionary. zh-CN is the default (target audience is
// mainland China users); English is the toggle. This avoids a full next-intl
// route-tree restructure — strings are looked up client-side via useT().
//
// Applied to public-facing pages (landing, pricing, models, docs, nav). The
// authenticated dashboard/admin remain English (internal tooling) for now.

export type Locale = 'zh-CN' | 'en'

export const messages = {
  'zh-CN': {
    'nav.models': '模型',
    'nav.pricing': '价格',
    'nav.docs': '文档',
    'nav.login': '登录',
    'nav.getStarted': '开始使用',
    'nav.dashboard': '控制台',

    'landing.heroTitle': '调用全球前沿大模型',
    'landing.heroSubtitle': '用支付宝 / 微信支付结算',
    'landing.heroBlurb': 'GPT-4o · Claude · Gemini —— 一个 API，一份账单，18% 透明加价',
    'landing.cta': '免费试用 $1 →',
    'landing.ctaNote': 'GitHub 登录无需邮箱验证',
    'landing.prop1Title': '前沿模型',
    'landing.prop1Body': '一个 OpenAI 兼容 API，自由切换',
    'landing.prop2Title': '支付宝 / 微信付款',
    'landing.prop2Body': '无需海外信用卡',
    'landing.prop3Title': '18% 平价加价',
    'landing.prop3Body': '无套餐，用多少付多少',

    'pricing.title': '价格',
    'pricing.subtitle': '预付费额度，用多少扣多少，18% 透明加价',
    'pricing.presetsTitle': '充值套餐',
    'pricing.perModelTitle': '模型单价',
    'pricing.colModel': '模型',
    'pricing.colInput': '输入 / 1M',
    'pricing.colOutput': '输出 / 1M',
    'pricing.faqTitle': '常见问题',

    'models.title': '支持的模型',
    'models.subtitle': '所有价格为供应商原价，结算时加 18%',
    'models.colModel': '模型',
    'models.colContext': '上下文',
    'models.colInput': '输入 / 1M',
    'models.colOutput': '输出 / 1M',
    'models.colCaps': '能力',

    'docs.title': 'API 文档',
    'docs.quickstart': '快速开始',

    'common.tools': '工具',
    'common.vision': '视觉',
    'common.streaming': '流式',
  },
  en: {
    'nav.models': 'Models',
    'nav.pricing': 'Pricing',
    'nav.docs': 'Docs',
    'nav.login': 'Log in',
    'nav.getStarted': 'Get Started',
    'nav.dashboard': 'Dashboard',

    'landing.heroTitle': 'Call frontier LLMs through one API',
    'landing.heroSubtitle': 'Settle with Alipay or WeChat Pay',
    'landing.heroBlurb': 'GPT-4o · Claude · Gemini — one API, one bill, transparent 18% markup',
    'landing.cta': 'Start with $1 free →',
    'landing.ctaNote': 'GitHub login needs no email verification',
    'landing.prop1Title': 'Frontier models',
    'landing.prop1Body': 'One OpenAI-compatible API, switch freely',
    'landing.prop2Title': 'Alipay / WeChat Pay',
    'landing.prop2Body': 'No overseas credit card needed',
    'landing.prop3Title': '18% flat markup',
    'landing.prop3Body': 'No plans — pay for what you use',

    'pricing.title': 'Pricing',
    'pricing.subtitle': 'Pre-paid credits, pay as you go, transparent 18% markup',
    'pricing.presetsTitle': 'Top-up amounts',
    'pricing.perModelTitle': 'Per-model rates',
    'pricing.colModel': 'Model',
    'pricing.colInput': 'Input / 1M',
    'pricing.colOutput': 'Output / 1M',
    'pricing.faqTitle': 'FAQ',

    'models.title': 'Supported models',
    'models.subtitle': 'Prices shown are provider cost; 18% is added at billing',
    'models.colModel': 'Model',
    'models.colContext': 'Context',
    'models.colInput': 'Input / 1M',
    'models.colOutput': 'Output / 1M',
    'models.colCaps': 'Capabilities',

    'docs.title': 'API Documentation',
    'docs.quickstart': 'Quickstart',

    'common.tools': 'Tools',
    'common.vision': 'Vision',
    'common.streaming': 'Streaming',
  },
} as const

export type MessageKey = keyof (typeof messages)['en']

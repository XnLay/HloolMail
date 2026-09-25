package apispec

// 三种公开契约复用同一段限流语义，避免客户端依据不同文档采取冲突的重试行为。
const apiRateLimitGuide = "API Key 请求同时受来源 IP、单实例入口和业务规则约束，管理员可在“API 接入”调整设置。" +
	"业务额度按 API Key、HTTP 方法和路由模板计数；查询参数和具体资源 ID 不会产生独立额度。多实例共享配置但各自计数。" +
	"速率超限返回 HTTP 429、rate limit exceeded 和 Retry-After 响应头；Retry-After 是至少等待的整数秒数，此类拒绝不扣每日或累计调用配额。" +
	"每日或累计配额耗尽也返回 429，但不带此重试提示，需要等待配额重置或由管理员调整配额。" +
	"客户端应遵守 Retry-After，使用带抖动且有次数上限的退避，避免立即并发重试；持续收件通知优先使用 Webhook。"

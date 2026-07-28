package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	patchFeatures = append(patchFeatures, "api-catalog")
}

type apiCatalogRoute struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Category       string `json:"category"`
	Summary        string `json:"summary"`
	Description    string `json:"description"`
	Authentication string `json:"authentication"`
	Permission     string `json:"permission"`
	Request        string `json:"request,omitempty"`
	Response       string `json:"response"`
	Handler        string `json:"handler"`
	Patched        bool   `json:"patched"`
}

type apiCatalogDescriptor struct {
	Category    string
	Summary     string
	Description string
	Permission  string
	Request     string
	Response    string
	Patched     bool
}

var apiCatalogExact = map[string]apiCatalogDescriptor{
	"GET /api/patch/info": {
		Category: "补丁", Summary: "查询补丁版本与兼容信息", Description: "返回上游版本、稳定补丁版本、验证状态、构建信息和已启用功能。", Permission: "public", Response: "补丁来源、target_version、verified、features 与 build 元数据。", Patched: true,
	},
	"GET /api/catalog": {
		Category: "系统", Summary: "查询全部运行时 API", Description: "枚举当前进程实际注册的全部 /api 路由，并附带用途、认证、权限、请求和返回说明。", Permission: "authenticated", Response: "按路径和方法排序的 routes 数组。", Patched: true,
	},
	"PUT /api/bases/:id/name": {
		Category: "世界存档", Summary: "设置据点显示名称", Description: "仅写入 PalPanel 元数据，不修改 Palworld 存档。", Permission: "server:control", Request: `JSON: {"name":"新的据点名称"}`, Response: "更新后的据点自定义名称。", Patched: true,
	},
	"DELETE /api/bases/:id/name": {
		Category: "世界存档", Summary: "恢复据点原始名称", Description: "删除 PalPanel 中保存的据点显示别名。", Permission: "server:control", Response: "恢复后的据点名称状态。", Patched: true,
	},
	"GET /api/bases/:id/storage": {
		Category: "世界存档", Summary: "查询据点仓库", Description: "返回据点关联容器、槽位、本地化物品和数量，保持只读。", Permission: "authenticated", Response: "容器汇总和逐槽位库存。", Patched: true,
	},
	"GET /api/bases/:id/workers": {
		Category: "世界存档", Summary: "查询基地工作帕鲁", Description: "按实例 ID 合并基地工作位和帕鲁索引详情。", Permission: "authenticated", Response: "工作帕鲁列表及等级、性别、状态和被动词条。", Patched: true,
	},
	"GET /api/bases/:id/feed-boxes": {
		Category: "世界存档", Summary: "查询基地饲料箱摘要", Description: "聚合普通和低温饲料箱中的物品分布。", Permission: "authenticated", Response: "饲料箱、占用格和物品汇总。", Patched: true,
	},
	"PUT /api/players/:id/annotation": {
		Category: "玩家", Summary: "保存玩家备注与标签", Description: "备注和标签按存档源保存在 PalPanel 数据库，不修改玩家存档。", Permission: "players:write", Request: `JSON: {"note":"管理备注","tags":["标签"]}`, Response: "更新后的 annotation。", Patched: true,
	},
	"DELETE /api/players/:id/annotation": {
		Category: "玩家", Summary: "删除玩家备注与标签", Description: "删除指定存档源下的玩家管理元数据。", Permission: "players:write", Response: "清空后的 annotation 状态。", Patched: true,
	},
	"GET /api/players": {
		Category: "玩家", Summary: "查询玩家列表", Description: "除官方存档字段外，补丁版附加备注、标签和 WorldID 隔离的在线历史。", Permission: "authenticated", Request: "Query: limit, offset, q 等列表参数。", Response: "玩家列表、分页摘要和索引状态。", Patched: true,
	},
	"GET /api/players/:id": {
		Category: "玩家", Summary: "查询玩家详情", Description: "附加管理备注、标签、累计在线和最近会话。", Permission: "authenticated", Response: "玩家详情和在线历史。", Patched: true,
	},
	"GET /api/guilds/:id": {
		Category: "世界存档", Summary: "查询公会详情", Description: "返回会长、成员、成员注释和关联基地。", Permission: "authenticated", Response: "公会成员与关联基地详情。", Patched: true,
	},
	"GET /api/inventory": {
		Category: "世界存档", Summary: "查询全服库存", Description: "只读聚合玩家、据点和未识别容器，并返回无人时段正向净变化状态。", Permission: "authenticated", Request: "Query: q, owner_type, category, sort, source_id。", Response: "物品聚合、位置明细、筛选项、索引状态和 unattended。", Patched: true,
	},
	"GET /api/pals": {
		Category: "世界存档", Summary: "查询帕鲁仓库", Description: "补丁版支持等级、星级、平均 IV、性别、位置、被动词条和排序参数。", Permission: "authenticated", Request: "Query: min_level, min_stars, min_iv_average, gender, location, passive, sort。", Response: "帕鲁列表、分页摘要和索引状态。", Patched: true,
	},
	"GET /api/patch/update/status": {
		Category: "补丁", Summary: "查询补丁热更新状态", Description: "返回当前补丁、可用稳定 Release 和最近更新任务状态。", Permission: "authenticated", Response: "补丁版本与更新可用性。", Patched: true,
	},
	"POST /api/patch/update/check": {
		Category: "补丁", Summary: "检查稳定补丁更新", Description: "重新解析匹配当前 PalPanel 正式版本的 verified stable Release。", Permission: "server:control", Response: "检查结果和候选 Release。", Patched: true,
	},
	"POST /api/patch/update": {
		Category: "补丁", Summary: "执行补丁热更新", Description: "校验 Release 资产后通过任务队列原子替换当前二进制。", Permission: "server:control", Response: "已创建的 patch_hot_update 任务。", Patched: true,
	},
	"POST /api/save-sources/import/inspect": {
		Category: "世界存档", Summary: "检查存档导入或房主档迁移", Description: "除标准存档检查外，可识别合作房主存档并准备 UID 重映射。", Permission: "server:control", Request: "multipart/form-data 或导入检查参数；房主迁移按页面生成参数。", Response: "候选世界、冲突和迁移计划。", Patched: true,
	},
	"GET /api/security/paldefender/starter-gift": {
		Category: "PalDefender", Summary: "查询新玩家礼包配置", Description: "按当前 WorldID 返回配置、模板目录索引和发放记录。", Permission: "authenticated", Response: "礼包配置、目录、任务和世界范围信息。", Patched: true,
	},
	"PUT /api/security/paldefender/starter-gift": {
		Category: "PalDefender", Summary: "保存新玩家礼包配置", Description: "保存物品、模板、批大小和启用状态；首次启用建立已有玩家基线。", Permission: "security:write", Request: "JSON: enabled, item_batch_size, pal_batch_size, items, pal_templates。", Response: "冻结并持久化后的配置。", Patched: true,
	},
	"POST /api/security/paldefender/starter-gift/grants/:id/retry": {
		Category: "PalDefender", Summary: "重试未完成礼包任务", Description: "从已持久化的未完成批次继续发放。", Permission: "security:write", Response: "更新后的发放任务。", Patched: true,
	},
	"DELETE /api/security/paldefender/starter-gift/grants/:id": {
		Category: "PalDefender", Summary: "重置礼包发放记录", Description: "删除指定记录；玩家需离线后重新进入才能再次触发。", Permission: "security:write", Response: "重置结果。", Patched: true,
	},
}

func (s Server) apiCatalog(router *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		routes := make([]apiCatalogRoute, 0)
		for _, route := range router.Routes() {
			if route.Path != "/api" && !strings.HasPrefix(route.Path, "/api/") {
				continue
			}
			descriptor := describeAPIRoute(route.Method, route.Path)
			routes = append(routes, apiCatalogRoute{
				Method:         route.Method,
				Path:           route.Path,
				Category:       descriptor.Category,
				Summary:        descriptor.Summary,
				Description:    descriptor.Description,
				Authentication: apiAuthentication(route.Method, route.Path),
				Permission:     descriptor.Permission,
				Request:        descriptor.Request,
				Response:       descriptor.Response,
				Handler:        shortHandlerName(route.Handler),
				Patched:        descriptor.Patched,
			})
		}
		sort.Slice(routes, func(i, j int) bool {
			if routes[i].Path == routes[j].Path {
				return routes[i].Method < routes[j].Method
			}
			return routes[i].Path < routes[j].Path
		})
		ok(c, gin.H{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"base_path":    "/api",
			"count":        len(routes),
			"authentication": gin.H{
				"session":         "浏览器登录会话 Cookie",
				"development_key": "PalPanel Development Key；具体传递方式以 docs/openapi.yaml 为准",
				"special":         "breed session 与 AstrBot 签名接口使用各自认证",
			},
			"routes": routes,
		})
	}
}

func describeAPIRoute(method, path string) apiCatalogDescriptor {
	if descriptor, found := apiCatalogExact[method+" "+path]; found {
		return descriptor
	}
	category, description := apiFamily(path)
	return apiCatalogDescriptor{
		Category:    category,
		Summary:     category + "接口",
		Description: description,
		Permission:  defaultAPIPermission(method, path),
		Response:    "标准 PalPanel JSON envelope；精确字段以 docs/openapi.yaml 和运行版本为准。",
	}
}

func apiFamily(path string) (string, string) {
	families := []struct {
		prefix      string
		category    string
		description string
	}{
		{"/api/auth", "认证", "登录、会话、密码和 Development Key 管理。"},
		{"/api/breed", "配种会话", "独立配种会话、预设、容器和任务接口。"},
		{"/api/integrations/astrbot", "AstrBot 集成", "由 AstrBot 签名调用的绑定、查询和控制接口。"},
		{"/api/community-servers", "社区服务器", "社区服务器列表、来源状态和刷新。"},
		{"/api/server", "服务器", "PalServer 安装、启动、停止、配置、日志、版本和官方 REST 代理。"},
		{"/api/monitor", "监控", "服务器监控快照和历史。"},
		{"/api/backups", "备份", "本地及 WebDAV 备份、恢复、验证和下载。"},
		{"/api/config", "配置", "Palworld 配置读取、验证和应用。"},
		{"/api/mods", "模组", "本地、上传、Workshop 和模组配置管理。"},
		{"/api/ai", "AI 翻译", "AI 翻译服务配置和连通性测试。"},
		{"/api/security/paldefender", "PalDefender", "PalDefender 安装、配置、访问控制和游戏内管理。"},
		{"/api/save", "世界存档", "存档源、索引状态和重建。"},
		{"/api/players", "玩家", "玩家列表、详情、背包、封禁、白名单和游戏内操作。"},
		{"/api/guilds", "世界存档", "公会列表与详情。"},
		{"/api/bases", "世界存档", "基地列表、详情、仓库和工作帕鲁。"},
		{"/api/pals", "世界存档", "帕鲁列表与详情。"},
		{"/api/inventory", "世界存档", "全服库存聚合。"},
		{"/api/map", "世界存档", "地图实体索引。"},
		{"/api/breeding", "配种", "管理端配种目录、预设和任务。"},
		{"/api/jobs", "系统", "异步任务列表和详情。"},
		{"/api/audit-logs", "审计", "操作审计记录。"},
		{"/api/settings", "系统", "面板网络和运行设置。"},
		{"/api/alerts", "系统", "告警查询和确认。"},
		{"/api/schedules", "系统", "计划任务管理。"},
		{"/api/patch", "补丁", "补丁来源和热更新。"},
	}
	for _, family := range families {
		if path == family.prefix || strings.HasPrefix(path, family.prefix+"/") {
			return family.category, family.description
		}
	}
	if path == "/api/health" || path == "/api/ready" {
		return "系统", "进程健康和就绪状态。"
	}
	return "其他", "当前 PalPanel 运行时注册的 API。"
}

func apiAuthentication(method, path string) string {
	if path == "/api/health" || path == "/api/ready" || path == "/api/patch/info" ||
		path == "/api/auth/status" || path == "/api/auth/register" || path == "/api/auth/login" {
		return "public"
	}
	if strings.HasPrefix(path, "/api/breed/") {
		if method == http.MethodPost && path == "/api/breed/session/exchange" {
			return "same-origin exchange"
		}
		return "breed-session"
	}
	if strings.HasPrefix(path, "/api/integrations/astrbot/") {
		return "astrbot-signature"
	}
	return "session-or-development-key"
}

func defaultAPIPermission(method, path string) string {
	if apiAuthentication(method, path) == "public" {
		return "public"
	}
	if method == http.MethodGet || method == http.MethodHead {
		return "authenticated"
	}
	return "route-specific; see docs/openapi.yaml"
}

func shortHandlerName(handler string) string {
	if index := strings.LastIndex(handler, "/"); index >= 0 {
		handler = handler[index+1:]
	}
	return strings.TrimSuffix(handler, "-fm")
}

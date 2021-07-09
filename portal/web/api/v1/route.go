package v1

import (
	"cloudiac/portal/libs/ctrl"
	"cloudiac/portal/web/api/v1/handlers"
	"cloudiac/portal/web/middleware"
	"github.com/gin-gonic/gin"
)

// @title 云霁 CloudIaC 基础设施即代码管理平台
// @version 1.0.0
// @description CloudIaC 是基于基础设施即代码构建的云环境自动化管理平台。
// @description CloudIaC 将易于使用的界面与强大的治理工具相结合，让您和您团队的成员可以快速轻松的在云中部署和管理环境。
// @description 通过将 CloudIaC 集成到您的流程中，您可以获得对组织的云使用情况的可见性、可预测性和更好的治理。

// @host localhost:9030
// @BasePath /api/v1
// @schemes http

// @securityDefinitions.apikey AuthToken
// @in header
// @name Authorization

func Register(g *gin.RouterGroup) {
	w := ctrl.GinRequestCtxWrap
	ac := middleware.AccessControl

	// 非授权用户相关路由
	g.Any("/check", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"success": true,
		})
	})
	g.POST("/auth/login", w(handlers.Auth{}.Login))

	// TODO 增加鉴权
	g.GET("/task/log/sse", w(handlers.Task{}.FollowLogSse))

	// Authorization Header 鉴权
	g.Use(w(middleware.Auth)) // 解析 header token

	ctrl.Register(g.Group("token", ac()), &handlers.Auth{})
	ctrl.Register(g.Group("system", ac()), &handlers.SystemConfig{})
	ctrl.Register(g.Group("webhook", ac()), &handlers.AccessToken{})
	g.GET("/auth/me", ac("self", "read"), w(handlers.Auth{}.GetUserByToken))
	g.PUT("/users/self", ac("self", "update"), w(handlers.User{}.UpdateSelf))
	g.GET("/runner/search", ac(), w(handlers.RunnerSearch))
	g.PUT("/consul/tags/update", ac(), w(handlers.ConsulTagUpdate))
	g.GET("/consul/kv/search", ac(), w(handlers.ConsulKVSearch))
	g.GET("/system/status/search", ac(), w(handlers.PortalSystemStatusSearch))

	ctrl.Register(g.Group("orgs", ac()), &handlers.Organization{})
	g.PUT("/orgs/:id/status", ac(), w(handlers.Organization{}.ChangeOrgStatus))
	g.GET("/orgs/:id/users", ac("orgs", "listuser"), w(handlers.Organization{}.SearchUser))
	g.PUT("/orgs/:id/users", ac("orgs", "adduser"), w(handlers.Organization{}.AddUserToOrg))
	g.DELETE("/orgs/:id/users/:userId", ac("orgs", "removeuser"), w(handlers.Organization{}.RemoveUserForOrg))
	g.PUT("/orgs/:id/users/:userId/role", ac("orgs", "updaterole"), w(handlers.Organization{}.UpdateUserOrgRel))

	// 组织 header
	g.Use(w(middleware.AuthOrgId))

	ctrl.Register(g.Group("users", ac()), &handlers.User{})
	g.PUT("/users/:id/status", ac(), w(handlers.User{}.ChangeUserStatus))
	g.POST("/users/:id/password/reset", ac(), w(handlers.User{}.PasswordReset))

	g.POST("/projects", ac(), w(handlers.Organization{}.Search))
	g.GET("/projects", ac(), w(handlers.Organization{}.Search))
	g.GET("/projects/:id", ac(), w(handlers.Organization{}.Search))

	// 项目资源
	// TODO: parse project header
	g.Use(w(middleware.AuthProjectId))

	ctrl.Register(g.Group("variables", ac()), &handlers.Template{})
	ctrl.Register(g.Group("envs", ac()), &handlers.Env{})
	g.PUT("/envs/:id/archive", ac(), w(handlers.Env{}.Archive))
	g.PUT("/envs/:id/tasks/current", ac(), w(handlers.Task{}.CurrentTask))
	g.POST("/envs/:id/deploy", ac(), w(handlers.Env{}.Deploy))
	g.POST("/envs/:id/destroy", ac(), w(handlers.Env{}.Destroy))
	g.GET("/envs/:id/resources", ac(), w(handlers.Env{}.SearchResources))
	g.GET("/envs/:id/variables", ac(), w(handlers.Env{}.Search))

	g.GET("/tasks", ac(), w(handlers.Task{}.Search))
	g.GET("/tasks/:id", ac(), w(handlers.Task{}.Detail))
	g.GET("/tasks/:id/log", ac(), w(handlers.Task{}.Log))
	g.GET("/tasks/:id/output", ac(), w(handlers.Task{}.Output))
	g.POST("/tasks/:id/approve", ac("tasks", "approve"), w(handlers.Task{}.TaskApprove))

	ctrl.Register(g.Group("template", ac()), &handlers.Template{})
	g.GET("/template/overview", ac(), w(handlers.Template{}.Overview))
	g.GET("/template/tfvars/search, ac()", w(handlers.TemplateTfvarsSearch))
	g.GET("/template/variable/search", ac(), w(handlers.TemplateVariableSearch))
	g.GET("/template/playbook/search", ac(), w(handlers.TemplatePlaybookSearch))
	g.GET("/template/state_list", ac(), w(handlers.Task{}.TaskStateListSearch))

	ctrl.Register(g.Group("vcs", ac()), &handlers.Vcs{})
	g.GET("/vcs/repo/search", ac(), w(handlers.Vcs{}.ListRepos))
	g.GET("/vcs/branch/search", ac(), w(handlers.Vcs{}.ListBranches))
	g.GET("/vcs/readme", ac(), w(handlers.Vcs{}.GetReadmeContent))

	ctrl.Register(g.Group("notification", ac()), &handlers.Notification{})
	ctrl.Register(g.Group("resource/account", ac()), &handlers.ResourceAccount{})
}

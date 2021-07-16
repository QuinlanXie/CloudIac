package apps

import (
	"cloudiac/common"
	"cloudiac/portal/consts/e"
	"cloudiac/portal/libs/ctx"
	"cloudiac/portal/models"
	"cloudiac/portal/models/forms"
	"cloudiac/portal/services"
	"cloudiac/utils"
	"fmt"
	"net/http"
	"strings"
)

// CreateEnv 创建环境
func CreateEnv(c *ctx.ServiceCtx, form *forms.CreateEnvForm) (*models.Env, e.Error) {
	c.AddLogField("action", fmt.Sprintf("create env %s", form.Name))

	if c.OrgId == "" || c.ProjectId == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}

	// 检查模板
	query := c.DB().Where("status = ?", models.Enable)
	tpl, err := services.GetTemplateById(query, form.TplId)
	if err != nil && err.Code() == e.TemplateNotExists {
		return nil, e.New(err.Code(), err, http.StatusBadRequest)
	} else if err != nil {
		c.Logger().Errorf("error get template, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}
	if form.TfVarsFile == "" {
		form.TfVarsFile = tpl.TfVarsFile
	}
	if form.PlayVarsFile == "" {
		form.PlayVarsFile = tpl.PlayVarsFile
	}
	if form.Playbook == "" {
		form.Playbook = tpl.Playbook
	}
	if form.Timeout == 0 {
		form.Timeout = common.TaskStepTimeoutDuration
	}

	tx := c.Tx()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	env, err := services.CreateEnv(tx, models.Env{
		OrgId:     c.OrgId,
		ProjectId: c.ProjectId,
		CreatorId: c.UserId,
		TplId:     form.TplId,

		Name:     form.Name,
		RunnerId: form.RunnerId,
		Status:   models.EnvStatusInactive,
		OneTime:  form.OneTime,

		// 模板参数
		TfVarsFile:   form.TfVarsFile,
		PlayVarsFile: form.PlayVarsFile,
		Playbook:     form.Playbook,
		KeyId:        form.KeyId,

		// TODO: triggers 触发器设置
		AutoApproval: form.AutoApproval,
		// TODO: 自动销毁设置

	})
	if err != nil && err.Code() == e.EnvAlreadyExists {
		_ = tx.Rollback()
		return nil, e.New(err.Code(), err, http.StatusBadRequest)
	} else if err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error creating env, err %s", err)
		return nil, e.New(err.Code(), err, http.StatusInternalServerError)
	}

	// 创建变量
	// 前端只传修改过或者新建的变量?，所有修改过的上层变量都会变成新的环境变量进行创建
	vars := form.Variables
	// vars, err := services.OperationVariable(tx, env.Id, form.Variables)
	env.Variables = vars

	// 创建任务
	_, err = services.CreateTask(tx, tpl, env, models.Task{
		Name:        models.Task{}.GetTaskNameByType(form.TaskType),
		Type:        form.TaskType,
		Flow:        models.TaskFlow{},
		Targets:     strings.Split(form.Targets, ","),
		CreatorId:   c.UserId,
		KeyId:       env.KeyId,
		RunnerId:    env.RunnerId,
		Variables:   form.Variables,
		StepTimeout: form.Timeout,
	})
	if err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error creating task, err %s", err)
		return nil, e.New(err.Code(), err, http.StatusInternalServerError)
	}

	// 创建完成
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error commit env, err %s", err)
		return nil, e.New(e.DBError, err)
	}

	return env, nil
}

// SearchEnv 环境查询
func SearchEnv(c *ctx.ServiceCtx, form *forms.SearchEnvForm) (interface{}, e.Error) {
	if c.OrgId == "" || c.ProjectId == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}
	query := c.DB().Where("iac_env.org_id = ? AND iac_env.project_id = ?", c.OrgId, c.ProjectId)
	query = services.QueryEnvDetail(query)

	if form.Status != "" {
		if utils.InArrayStr(models.EnvStatus, form.Status) {
			return nil, e.New(e.BadParam, http.StatusBadRequest)
		}
		query = query.Where("iac_env.status = ?", form.Status)
	}

	// 环境归档状态
	switch form.Archived {
	case "":
		// 默认返回未归档环境
		query = query.Where("iac_env.archived = ?", 0)
	case "all":
	// do nothing
	case "true":
		// 已归档
		query = query.Where("iac_env.archived == 1")
	case "false":
		// 未归档
		query = query.Where("iac_env.archived == 0")
	default:
		return nil, e.New(e.BadParam, http.StatusBadRequest)
	}

	if form.Q != "" {
		query = query.WhereLike("iac_env.name", form.Q)
	}

	// 默认按创建时间逆序排序
	if form.SortField() == "" {
		query = query.Order("iac_env.created_at DESC")
	}

	rs, err := getPage(query, form, &models.EnvDetail{})
	if err != nil {
		c.Logger().Errorf("error get page, err %s", err)
	}
	return rs, err
}

// UpdateEnv 环境编辑
func UpdateEnv(c *ctx.ServiceCtx, form *forms.UpdateEnvForm) (*models.Env, e.Error) {
	c.AddLogField("action", fmt.Sprintf("update env %s", form.Id))
	if c.OrgId == "" || c.ProjectId == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}
	query := c.DB().Where("iac_env.org_id = ? AND iac_env.project_id = ?", c.OrgId, c.ProjectId)

	env, err := services.GetEnvById(query, form.Id)
	if err != nil && err.Code() != e.EnvNotExists {
		return nil, e.New(err.Code(), err, http.StatusNotFound)
	} else if err != nil {
		c.Logger().Errorf("error get env, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}

	// 项目已归档，不允许编辑
	if env.Archived && form.Archived == false {
		return nil, e.New(e.EnvArchived, http.StatusBadRequest)
	}

	attrs := models.Attrs{}
	if form.HasKey("name") {
		attrs["name"] = form.Name
	}

	if form.HasKey("description") {
		attrs["description"] = form.Description
	}

	if form.HasKey("keyId") {
		attrs["key_id"] = form.KeyId
	}

	if form.HasKey("runnerId") {
		attrs["runner_id"] = form.RunnerId
	}

	if form.HasKey("autoApproval") {
		attrs["auto_approval"] = form.AutoApproval
	}

	if form.HasKey("autoDestroyAt") {
		// TODO: 修改生命周期
	}

	if form.HasKey("archived") {
		if env.Status != models.EnvStatusInactive {
			return nil, e.New(e.EnvCannotArchiveActive,
				fmt.Errorf("env can't be archive while env is %s", env.Status),
				http.StatusBadRequest)
		}
		attrs["archived"] = form.Archived
	}

	env, err = services.UpdateEnv(c.DB(), form.Id, attrs)
	if err != nil && err.Code() == e.EnvAliasDuplicate {
		return nil, e.New(err.Code(), err, http.StatusBadRequest)
	} else if err != nil {
		c.Logger().Errorf("error update env, err %s", err)
		return nil, err
	}
	return env, nil
}

// EnvDetail 环境信息详情
func EnvDetail(c *ctx.ServiceCtx, form forms.DetailEnvForm) (*models.EnvDetail, e.Error) {
	if c.OrgId == "" || c.ProjectId == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}
	query := c.DB().Where("iac_env.org_id = ? AND iac_env.project_id = ?", c.OrgId, c.ProjectId)
	query = services.QueryEnvDetail(query)

	envDetail, err := services.GetEnvDetailById(query, form.Id)
	if err != nil && err.Code() == e.EnvNotExists {
		return nil, e.New(e.EnvNotExists, err, http.StatusNotFound)
	} else if err != nil {
		c.Logger().Errorf("error get env by id, err %s", err)
		return nil, e.New(e.DBError, err)
	}

	return envDetail, nil
}

// EnvDeploy 创建新部署任务
// 任务类型：apply, destroy
func EnvDeploy(c *ctx.ServiceCtx, form *forms.DeployEnvForm) (*models.Env, e.Error) {
	c.AddLogField("action", fmt.Sprintf("create env task %s", form.Id))
	if c.OrgId == "" || c.ProjectId == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}

	tx := c.Tx().Where("iac_env.org_id = ? AND iac_env.project_id = ?", c.OrgId, c.ProjectId)
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	env, err := services.GetEnvById(tx, form.Id)
	if err != nil && err.Code() != e.EnvNotExists {
		return nil, e.New(err.Code(), err, http.StatusNotFound)
	} else if err != nil {
		c.Logger().Errorf("error get env, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}

	// env 状态检查
	if env.Archived {
		return nil, e.New(e.EnvArchived, http.StatusBadRequest)
	}
	if env.Deploying {
		return nil, e.New(e.EnvDeploying, http.StatusBadRequest)
	}

	// 模板检查
	tpl, err := services.GetTemplateById(tx, env.TplId)
	if err != nil && err.Code() == e.TemplateNotExists {
		return nil, e.New(err.Code(), err, http.StatusBadRequest)
	} else if err != nil {
		c.Logger().Errorf("error get template, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}
	if tpl.Status == models.Disable {
		return nil, e.New(e.TemplateDisabled, http.StatusBadRequest)
	}

	if form.TfVarsFile == "" {
		form.TfVarsFile = tpl.TfVarsFile
	}
	if form.PlayVarsFile == "" {
		form.PlayVarsFile = tpl.PlayVarsFile
	}
	if form.Playbook == "" {
		form.Playbook = tpl.Playbook
	}

	// 变量
	// TODO: 检查、保存、合并环境变量
	//vars := form.Variables

	if form.HasKey("name") {
		env.Name = form.Name
	}
	if form.HasKey("autoApproval") {
		env.AutoApproval = form.AutoApproval
	}
	if form.HasKey("autoDestroyAt") {
		// TODO: 自动销毁设置
	}
	if form.HasKey("keyId") {
		env.KeyId = form.KeyId
	}
	if form.HasKey("runnerId") {
		env.RunnerId = form.RunnerId
	}
	if form.HasKey("timeout") {
		env.Timeout = form.Timeout
	}
	if form.HasKey("variables") {
		env.Variables = form.Variables
	}
	if form.HasKey("tfVarsFile") {
		env.TfVarsFile = form.TfVarsFile
	}
	if form.HasKey("playVarsFile") {
		env.PlayVarsFile = form.PlayVarsFile
	}
	if form.HasKey("playbook") {
		env.Playbook = form.Playbook
	}
	if form.TaskType == "" {
		return nil, e.New(e.BadParam, http.StatusBadRequest)
	}

	// 创建任务
	_, err = services.CreateTask(tx, tpl, env, models.Task{
		Name:        models.Task{}.GetTaskNameByType(form.TaskType),
		Type:        form.TaskType,
		Flow:        models.TaskFlow{},
		Targets:     strings.Split(form.Targets, ","),
		CreatorId:   c.UserId,
		KeyId:       env.KeyId,
		RunnerId:    env.RunnerId,
		Variables:   form.Variables,
		StepTimeout: form.Timeout,
	})

	if err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error creating task, err %s", err)
		return nil, e.New(err.Code(), err, http.StatusInternalServerError)
	}

	if _, err := tx.Save(env); err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error save env, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		c.Logger().Errorf("error commit env, err %s", err)
		return nil, e.New(e.DBError, err)
	}

	return env, nil
}

// SearchEnvResources 查询环境资源列表
func SearchEnvResources(c *ctx.ServiceCtx, form *forms.SearchEnvResourceForm) (interface{}, e.Error) {
	if c.OrgId == "" || c.ProjectId == "" || form.Id == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}

	env, err := services.GetEnvById(c.DB(), form.Id)
	if err != nil && err.Code() != e.EnvNotExists {
		return nil, e.New(err.Code(), err, http.StatusNotFound)
	} else if err != nil {
		c.Logger().Errorf("error get env, err %s", err)
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}

	query := c.DB().Model(models.Resource{}).Where("org_id = ? AND project_id = ? AND env_id = ?",
		c.OrgId, c.ProjectId, form.Id, env.LastTaskId)

	if form.HasKey("q") {
		// 支持对 provider / type / name 进行模糊查询
		query = query.Where("provider LIKE ? OR type LIKE ? OR name LIKE ?",
			fmt.Sprintf("%%%s%%", form.Q),
			fmt.Sprintf("%%%s%%", form.Q),
			fmt.Sprintf("%%%s%%", form.Q))
	}

	if form.SortField() == "" {
		query = query.Order("provider, type, name")
	}

	return getPage(query, form, &models.Resource{})
}

// SearchEnvVariables 查询环境变量列表
func SearchEnvVariables(c *ctx.ServiceCtx, form *forms.SearchEnvVariableForm) (interface{}, e.Error) {
	if c.OrgId == "" || c.ProjectId == "" || form.Id == "" {
		return nil, e.New(e.BadRequest, http.StatusBadRequest)
	}
	query := c.DB().Where("org_id = ? AND project_id = ? AND id = ?", c.OrgId, c.ProjectId, form.Id)
	env, err := services.GetEnvById(query, form.Id)
	if err != nil && err.Code() == e.EnvNotExists {
		return nil, e.New(e.EnvNotExists, err, http.NotFound)
	} else if err != nil {
		c.Logger().Errorf("error while get env by id, err %s", err)
		return nil, e.New(e.DBError, err)
	}

	return env.Variables, nil
}
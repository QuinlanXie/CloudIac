package apps

import (
	"cloudiac/portal/consts/e"
	"cloudiac/portal/libs/ctx"
	"cloudiac/portal/libs/page"
	"cloudiac/portal/models"
	"cloudiac/portal/models/forms"
	"cloudiac/portal/services"
)

func CreateProject(c *ctx.ServiceCtx, form *forms.CreateProjectForm) (interface{}, e.Error) {
	tx := c.DB().Begin()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	project, err := services.CreateProject(tx, &models.Project{
		Name:        form.Name,
		OrgId:       c.OrgId,
		Description: form.Description,
		CreatorId:   c.UserId,
	})
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := services.BindProjectUsers(tx, project.Id, form.UserAuthorization); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, e.New(e.DBError, err)
	}
	return project, nil
}

type ProjectResp struct {
	models.Project
	Creator string `json:"creator" form:"creator" `
}

func SearchProject(c *ctx.ServiceCtx, form *forms.SearchProjectForm) (interface{}, e.Error) {
	query := services.SearchProject(c.DB(), c.OrgId, form.Q)
	// 默认按创建时间逆序排序
	if form.SortField() == "" {
		query = query.Order("created_at DESC")
	}
	p := page.New(form.CurrentPage(), form.PageSize(), query)
	projectResp := make([]ProjectResp, 0)
	if err := p.Scan(&projectResp); err != nil {
		return nil, e.New(e.DBError, err)
	}
	for _, v := range projectResp {
		user, _ := services.GetUserById(c.DB(), v.CreatorId)
		v.Creator = user.Name
	}

	return page.PageResp{
		Total:    p.MustTotal(),
		PageSize: p.Size,
		List:     projectResp,
	}, nil
}

func UpdateProject(c *ctx.ServiceCtx, form *forms.UpdateProjectForm) (interface{}, e.Error) {
	// 先删除项目和用户关系，在重新创建
	tx := c.DB().Begin()
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()
	if err := services.UpdateProjectUsers(tx, form.Id, form.UserAuthorization); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	//修改项目数据
	attrs := models.Attrs{}
	if form.HasKey("name") {
		attrs["name"] = form.Name
	}

	if form.HasKey("description") {
		attrs["description"] = form.Description
	}
	project := &models.Project{}
	project.Id = form.Id
	if err := services.UpdateProject(tx, project, attrs); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, e.New(e.DBError, err)
	}
	return nil, nil
}

func DeleteProject(c *ctx.ServiceCtx, form *forms.DeleteProjectForm) (interface{}, e.Error) {
	return nil, e.New(e.NotImplement)
	//tx := c.DB().Begin()
	//defer func() {
	//	if r := recover(); r != nil {
	//		_ = tx.Rollback()
	//		panic(r)
	//	}
	//}()
	////todo 检验环境是否活跃
	////项目是逻辑删除，用户和项目的角色关系是直接删除
	//if err := services.DeleteProject(tx, form.Id); err != nil {
	//	_ = tx.Rollback()
	//	return nil, err
	//}
	//
	//if err := services.DeleteUserProject(tx, form.Id); err != nil {
	//	_ = tx.Rollback()
	//	return nil, err
	//}
	//
	//if err := tx.Commit(); err != nil {
	//	_ = tx.Rollback()
	//	return nil, e.New(e.DBError, err)
	//}
	//
	//return nil, nil
}

func DetailProject(c *ctx.ServiceCtx, form *forms.DetailProjectForm) (interface{}, e.Error) {
	return services.DetailProject(c.DB(), form.Id)
}

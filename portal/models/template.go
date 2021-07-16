package models

import "cloudiac/portal/libs/db"

type Template struct {
	SoftDeleteModel

	Name        string `json:"name" gorm:"not null;comment:'模板名称'" example:"yunji_example"`
	TplType     string `json:"tplType" gorm:"not null;comment:'云模板类型(aliyun，VMware等)'" example:"aliyun"`
	OrgId       Id     `json:"orgId" gorm:"size:32;not null" example:"a1f79e8a-744d-4ea5-8d97-7e4b7b422a6c"`
	Description string `json:"description" gorm:"type:text" example:"云霁阿里云模板"`
	VcsId       Id     `json:"vcsId" gorm:"size:32;not null" example:"a1f79e8a-744d-4ea5-8d97-7e4b7b422a6c"`

	// RepoId 和 RepoAddr 只能选其一赋值，当设置了 RepoAddr 时 RepoId 被忽略，
	// 此时创建任务直接使用 RepoRevision 做为 commit id，不再实时获取
	RepoId   string `json:"repoId" gorm:"not null"`                                                 // RepoId 仓库 id 或者 path(local vcs)
	RepoAddr string `json:"repoAddr" gorm:"not null" example:"https://github.com/user/project.git"` // RepoAddr 直接指定仓库地址

	RepoToken    string `json:"repoToken" gorm:"size:128" ` // RepoToken 若为空则使用 vcs 的 token
	RepoRevision string `json:"repoRevision" gorm:"size:64;default:'master'" example:"master"`

	Status     string `json:"status" gorm:"type:enum('enable','disable');default:'enable';comment:'状态'"`
	CreatorId  Id     `json:"creatorId" gorm:"size:32;not null;comment:'创建人'"`
	Workdir    string `json:"workdir" gorm:"default:''" example:"aws"` // 基于项目根目录的相对路径, 默认为空
	TfVarsFile string `json:"tfVarsFile" gorm:"default:''"`            // Terraform 变量文件路径

	// 要执行的 ansible playbook 文件(基于 workdir 的相对路径)
	Playbook     string `json:"playbook" gorm:"default:''" example:"ansbile/playbook.yml"`
	PlayVarsFile string `json:"playVarsFile" gorm:"default:''"` // Ansible 变量文件路径
}

func (Template) TableName() string {
	return "iac_template"
}

func (t *Template) Migrate(sess *db.Session) (err error) {
	if err = sess.RemoveIndex("iac_template", "unique__project__tpl__name"); err != nil {
		return err
	}
	if err = t.AddUniqueIndex(sess, "unique__org__tpl__name", "org_id", "name"); err != nil {
		return err
	}
	return nil
}

// TODO 改用统一的 ApiToken 表
type TemplateAccessToken struct {
	TimedModel

	TplGuid     string `json:"tplGuid" form:"tplGuid" gorm:"not null"`
	AccessToken string `json:"accessToken" form:"accessToken" gorm:"not null"`
	Action      string `json:"action" form:"action"  gorm:"type:enum('plan','apply','compliance');default:'plan'"`
}

func (TemplateAccessToken) TableName() string {
	return "iac_template_access_token"
}

func (o TemplateAccessToken) Migrate(sess *db.Session) (err error) {
	err = o.AddUniqueIndex(sess, "unique__guid", "access_token")
	if err != nil {
		return err
	}

	return nil
}
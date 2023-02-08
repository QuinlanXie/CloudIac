// Copyright (c) 2015-2023 CloudJ Technology Co., Ltd.

package apps

import (
	"cloudiac/common"
	"cloudiac/portal/consts"
	"cloudiac/portal/consts/e"
	"cloudiac/portal/libs/ctx"
	"cloudiac/portal/libs/db"
	"cloudiac/portal/models"
	"cloudiac/portal/models/forms"
	"cloudiac/portal/models/resps"
	"cloudiac/portal/services"
	"fmt"
	"net/http"
	"strings"
)

func SearchPolicySuppress(c *ctx.ServiceContext, form *forms.SearchPolicySuppressForm) (interface{}, e.Error) {
	query := services.SearchPolicySuppress(c.DB(), form.Id, c.OrgId)
	if form.SortField() == "" {
		query = query.Order(fmt.Sprintf("%s.created_at DESC", resps.PolicySuppressResp{}.TableName()))
	}
	return getPage(query, form, resps.PolicySuppressResp{})
}

func UpdatePolicySuppress(c *ctx.ServiceContext, form *forms.UpdatePolicySuppressForm) (interface{}, e.Error) {
	c.AddLogField("action", fmt.Sprintf("update policy suppress %s", form.Id))
	var (
		sups []models.PolicySuppress
		err e.Error
	)
	_ = c.DB().Transaction(func(tx *db.Session) error {
		tx = services.QueryWithOrgId(tx, c.OrgId)
		for _, id := range form.AddSourceIds {
			// 权限检查
			if err := AllowAccessResource(tx, c, id); err != nil {
				return  err
			}
		}
		// 创新新的屏蔽记录
		for _, id := range form.AddSourceIds {
			if strings.HasPrefix(string(id), "env-") {
				env, _ := services.GetEnvById(tx, id)
				if env.OrgId != c.OrgId {
					c.Logger().Errorf("env do not belong to org, env: %s, org: %s", env.Id, c.OrgId)
					continue
				}
				sups = append(sups, models.PolicySuppress{
					CreatorId:  c.UserId,
					OrgId:      c.OrgId,
					ProjectId:  env.ProjectId,
					TargetId:   id,
					TargetType: consts.ScopeEnv,
					PolicyId:   form.Id,
					Type:       common.PolicySuppressTypeSource,
					Reason:     form.Reason,
				})
			} else if strings.HasPrefix(string(id), "tpl-") {
				tpl, _ := services.GetTemplateById(tx, id)
				if tpl.OrgId != c.OrgId {
					c.Logger().Errorf("tpl do not belong to org, env: %s, org: %s", tpl.Id, c.OrgId)
					continue
				}
				sups = append(sups, models.PolicySuppress{
					CreatorId:  c.UserId,
					OrgId:      c.OrgId,
					TargetId:   id,
					TargetType: consts.ScopeTemplate,
					PolicyId:   form.Id,
					Type:       common.PolicySuppressTypeSource,
					Reason:     form.Reason,
				})
			} else if strings.HasPrefix(string(id), "po-") {
				// 一次只能提交一个策略禁用
				if len(form.AddSourceIds) > 1 {
					return e.New(e.BadParam, fmt.Errorf("one policy id a time"), http.StatusBadRequest)
				}
				if form.Id != id {
					return e.New(e.BadParam, fmt.Errorf("invalid policy id to disable"), http.StatusBadRequest)
				}
				po, _ := services.GetPolicyById(tx, id, c.OrgId)
				sups = append(sups, models.PolicySuppress{
					CreatorId:  c.UserId,
					TargetId:   id,
					TargetType: consts.ScopePolicy,
					PolicyId:   form.Id,
					Type:       common.PolicySuppressTypePolicy,
					Reason:     form.Reason,
					OrgId:      c.OrgId,
				})
				// 禁用此策略在添加屏蔽的同时设置策略状态为禁用
				po.Enabled = false
				if _, err := tx.Save(po); err != nil {
					_ = tx.Rollback()
					return  e.New(e.DBError, err)
				}
			}
		}

		if er := models.CreateBatch(tx, sups); er != nil {
			_ = tx.Rollback()
			if e.IsDuplicate(er) {
				return  e.New(e.PolicySuppressAlreadyExist, er, http.StatusBadRequest)
			}
			return  e.New(e.DBError, er)
		}

		return err
	})

	return sups, nil
}

func DeletePolicySuppress(c *ctx.ServiceContext, form *forms.DeletePolicySuppressForm) (interface{}, e.Error) {
	tx := services.QueryWithOrgId(c.Tx(), c.OrgId)
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	sup, err := services.GetPolicySuppressById(tx, form.SuppressId)
	if err != nil {
		c.Logger().Errorf("sup not exist, rollback, code %d", err.Code())
		_ = tx.Rollback()
		if err.Code() == e.PolicySuppressNotExist {
			return nil, e.New(err.Code(), err, http.StatusBadRequest)
		}
		return nil, e.New(err.Code(), err, http.StatusInternalServerError)
	}
	if sup.TargetType == consts.ScopePolicy {
		_, err := services.PolicyEnable(tx, sup.TargetId, true, c.OrgId)
		if err != nil {
			_ = tx.Rollback()
			return nil, e.New(err.Code(), err, http.StatusInternalServerError)
		}
	}

	_, err = services.DeletePolicySuppress(tx, form.SuppressId)
	if err != nil {
		_ = tx.Rollback()
		if err.Code() == e.PolicySuppressNotExist {
			return nil, e.New(err.Code(), err, http.StatusBadRequest)
		}
		return nil, e.New(err.Code(), err, http.StatusInternalServerError)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, e.New(e.DBError, err, http.StatusInternalServerError)
	}

	return nil, nil
}

func AllowAccessResource(tx *db.Session, c *ctx.ServiceContext, id models.Id) e.Error {
	if strings.HasPrefix(string(id), "env-") {
		env, err := services.GetEnvById(tx, id)
		if err != nil {
			_ = tx.Rollback()
			if err.Code() == e.EnvNotExists {
				return e.New(err.Code(), err, http.StatusBadRequest)
			}
			return e.New(e.DBError, err, http.StatusInternalServerError)
		}

		if !c.IsSuperAdmin && !services.UserHasOrgRole(c.UserId, env.OrgId, consts.OrgRoleAdmin) &&
			!services.UserHasProjectRole(c.UserId, env.OrgId, env.ProjectId, "") {
			_ = tx.Rollback()
			return e.New(e.EnvNotExists, fmt.Errorf("cannot access env %s", id), http.StatusForbidden)
		}
	} else if strings.HasPrefix(string(id), "tpl-") {
		tpl, err := services.GetTemplateById(tx, id)
		if err != nil {
			_ = tx.Rollback()
			if err.Code() == e.TemplateNotExists {
				return e.New(err.Code(), err, http.StatusBadRequest)
			}
			return e.New(e.DBError, err, http.StatusInternalServerError)
		}
		if !c.IsSuperAdmin && !services.UserHasOrgRole(c.UserId, tpl.OrgId, "") {
			_ = tx.Rollback()
			return e.New(e.TemplateNotExists, fmt.Errorf("cannot access tpl %s", id), http.StatusForbidden)
		}
	} else if strings.HasPrefix(string(id), "po-") {
		_, err := services.GetPolicyById(tx, id, c.OrgId)
		if err != nil {
			_ = tx.Rollback()
			if err.Code() == e.PolicyNotExist {
				return e.New(err.Code(), err, http.StatusBadRequest)
			}
			return e.New(e.DBError, err, http.StatusInternalServerError)
		}
	}
	return nil
}

func SearchPolicySuppressSource(c *ctx.ServiceContext, form *forms.SearchPolicySuppressSourceForm) (interface{}, e.Error) {
	policy, err := services.GetPolicyById(c.DB(), form.Id, c.OrgId)
	if err != nil {
		if err.Code() == e.PolicyNotExist {
			return nil, e.New(err.Code(), err, http.StatusBadRequest)
		} else {
			return nil, e.New(err.Code(), err, http.StatusInternalServerError)
		}
	}
	query := services.SearchPolicySuppressSource(c.DB(), form, c.UserId, form.Id, policy.GroupId, c.OrgId)
	return getPage(query, form, resps.PolicySuppressSourceResp{})
}

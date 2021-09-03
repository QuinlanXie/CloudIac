package services

import (
	"cloudiac/common"
	"cloudiac/portal/consts/e"
	"cloudiac/portal/libs/db"
	"cloudiac/portal/models"
	"fmt"
	"time"
)

func GetPolicyResultById(query *db.Session, taskId models.Id, policyId models.Id) (*models.PolicyResult, e.Error) {
	result := models.PolicyResult{}
	if err := query.Model(models.PolicyResult{}).Where("task_id = ? AND policy_id = ?", taskId, policyId).First(&result); err != nil {
		if e.IsRecordNotFound(err) {
			return nil, e.New(e.PolicyResultNotExist, err)
		}
		return nil, e.New(e.DBError, err)
	}

	return &result, nil
}

// InitScanResult 初始化扫描结果
// FIXME: task 支持 task 和 scantask
func InitScanResult(tx *db.Session, task models.Task) e.Error {
	var (
		policies      []models.Policy
		err           e.Error
		policyResults []*models.PolicyResult
	)

	// 根据扫描类型获取策略列表
	if task.EnvId != "" {
		if policies, err = GetPoliciesByEnvId(tx, task.EnvId); err != nil {
			return err
		}
	} else {
		var env *models.Env
		if env, err = GetEnvById(tx, task.EnvId); err != nil {
			return err
		}
		if policies, err = GetPoliciesByTemplateId(tx, env.TplId); err != nil {
			return err
		}
	}

	// 批量创建
	for _, policy := range policies {
		policyResults = append(policyResults, &models.PolicyResult{
			OrgId:     task.OrgId,
			ProjectId: task.ProjectId,
			TplId:     task.TplId,
			EnvId:     task.EnvId,
			TaskId:    task.Id,

			PolicyId:      policy.Id,
			PolicyGroupId: policy.GroupId,

			StartAt: models.Time(time.Now()),
			Status:  "pending", // 设置结果为等待扫描状态
		})
	}

	if er := models.CreateBatch(tx, policyResults); er != nil {
		return e.New(e.DBError, er)
	}

	return nil
}

// UpdateScanResult 根据 terrascan 扫描结果批量更新
func UpdateScanResult(tx *db.Session, task models.Tasker, result TsResult) e.Error {
	var (
		policyResults []*models.PolicyResult
	)
	for _, r := range result.Violations {
		if policyResult, err := GetPolicyResultById(tx, task.GetId(), models.Id(r.RuleId)); err != nil {
			return err
		} else {
			policyResult.Status = "violated"
			policyResult.Line = r.Line
			policyResult.Source = r.Source
			policyResult.PlanRoot = r.PlanRoot
			policyResult.ModuleName = r.ModuleName
			policyResult.File = r.File
			policyResult.Violation = models.Violation{
				RuleName:     r.RuleName,
				Description:  r.Description,
				RuleId:       r.RuleId,
				Severity:     r.Severity,
				Category:     r.Category,
				Comment:      r.Comment,
				ResourceName: r.ResourceName,
				ResourceType: r.ResourceType,
				ModuleName:   r.ModuleName,
				File:         r.File,
				PlanRoot:     r.PlanRoot,
				Line:         r.Line,
				Source:       r.Source,
			}
			policyResults = append(policyResults, policyResult)
		}
	}
	for _, r := range result.PassedRules {
		if policyResult, err := GetPolicyResultById(tx, task.GetId(), models.Id(r.RuleId)); err != nil {
			return err
		} else {
			policyResult.Status = common.PolicyStatusPassed
			policyResults = append(policyResults, policyResult)
		}
	}
	for _, r := range policyResults {
		if err := models.Save(tx, r); err != nil {
			return e.New(e.DBError, fmt.Errorf("save scan result"))
		}
	}

	if err := finishScanResult(tx, task); err != nil {
		return err
	}
	return nil
}

// finishScanResult 更新状态未知的策略扫描结果为 failed
// 当存在相同名称当策略时，扫描结果可能不包含在结果集里面，这些策略扫描结果一律标记为 failed
func finishScanResult(tx *db.Session, task models.Tasker) e.Error {
	sql := fmt.Sprintf("UPDATE %s SET status = 'failed' WHERE task_id = ? AND status = 'pending'",
		models.PolicyResult{}.TableName())
	if _, err := tx.Exec(sql, task.GetId()); err != nil {
		return e.New(e.DBError, err)
	}
	return nil
}

/*
获取扫描结果

查询最新扫描结果：
按 org, policy id, policy group id，按 start at 取最新一条记录
根据范围做 distinct:
1. 查看策略
2. 查看策略组
3. 查看环境
4. 查看云模板

1. 策略页面获取当前策略扫描状态
2. 策略组页面获取当前策略组扫描状态
	是否扫描中：
		检查是否存在 result.status = pending
	是否违反：
		检查是否存在 result.status = violated
	是否存在错误：

'passed','violated','suppressed','pending','failed'
*/

func GetPolicyResultByPolicyId(query *db.Session, policyId models.Id) error {
	return nil
}

func GetLastScanTask(query *db.Session, envId models.Id, tplId models.Id) (*models.ScanTask, e.Error) {
	scanTask := models.ScanTask{}
	q := query.Model(models.ScanTask{})
	if envId != "" {
		q = q.Where("env_id = ?", envId)
	} else {
		q = q.Where("tpl_id = ?", tplId)
	}
	if err := q.Last(&scanTask); err != nil {
		if e.IsRecordNotFound(err) {
			return nil, e.New(e.TaskNotExists, err)
		}
		return nil, e.New(e.DBError, fmt.Errorf("query scan error: %v", err))
	}
	return &scanTask, nil
}

func GetPolicyGroupScanTasks(query *db.Session, policyGroupId models.Id) *db.Session {
	t := models.PolicyResult{}.TableName()
	subQuery := query.Model(models.PolicyResult{}).
		Select(fmt.Sprintf("if(%s.env_id='','template','environment')as target_type,if(%s.env_id = '',iac_template.name,iac_env.name) as target_name,%s.task_id,%s.policy_group_id", t, t, t, t)).
		Joins("LEFT JOIN iac_env ON iac_policy_result.env_id = iac_env.id").
		Joins("LEFT JOIN iac_template ON iac_policy_result.tpl_id = iac_template.id").
		Where("iac_policy_result.policy_group_id = ?", policyGroupId)

	q := query.Model(models.ScanTask{}).
		Joins("LEFT JOIN (?) AS r ON r.task_id = iac_scan_task.id", subQuery.Expr()).
		Where("r.policy_group_id = ?", policyGroupId).
		LazySelectAppend("iac_scan_task.*,r.*")

	// 创建者
	q = q.Joins("left join iac_user as u on u.id = iac_scan_task.creator_id").
		LazySelectAppend("u.name as creator")
	// 组织
	q = q.Joins("left join iac_org as o on o.id = iac_scan_task.org_id").
		LazySelectAppend("o.name as org_name")
	// 项目
	q = q.Joins("left join iac_project as p on p.id = iac_scan_task.project_id").
		LazySelectAppend("p.name as project_name")
	return q
}

func QueryPolicyResult(query *db.Session, taskId models.Id) *db.Session {
	q := query.Model(models.PolicyResult{}).Where("task_id = ?", taskId)

	// 策略信息
	q = q.Joins("left join iac_policy as p on p.id = iac_policy_result.policy_id").
		LazySelectAppend("p.name as policy_name, p.fix_suggestion,iac_policy_result.*")
	// 策略组信息
	q = q.Joins("left join iac_policy_group as g on g.id = iac_policy_result.policy_group_id").
		LazySelectAppend("g.name as policy_group_name,iac_policy_result.*")

	return q
}

//GetMirrorScanTask 查找部署任务对应的扫描任务
func GetMirrorScanTask(query *db.Session, taskId models.Id) (*models.ScanTask, e.Error) {
	t := models.ScanTask{}
	if err := query.Where("mirror = 1 AND mirror_task_id = ?", taskId).First(&t); err != nil {
		if e.IsRecordNotFound(err) {
			return nil, e.New(e.TaskNotExists, err)
		}
		return nil, e.New(e.DBError, err)
	}
	return &t, nil
}

package service

import (
	"context"
	"strings"

	"github.com/company/monitor-webserver/internal/apperr"
	"github.com/company/monitor-webserver/internal/dto"
	"github.com/company/monitor-webserver/internal/logic"
	"github.com/company/monitor-webserver/internal/model"
	"github.com/company/monitor-webserver/internal/util"
)

// OnlineService 提供在线用户（登录会话）查询与强制下线。
//
// 数据来源：sa_system_refresh_token（未吊销且未过期的刷新令牌即一个在线会话），
// 因此「会话编号」就是刷新令牌主键，也是 access token 载荷里的 sid。
type OnlineService struct {
	// auth 认证领域操作（会话读写）。
	auth *logic.AuthLogic
	// auths 认证应用服务（登记会话失效标记）。
	auths *AuthService
}

// NewOnlineService 创建在线用户应用服务。
//
// 参数 Parameters:
//   - auth (*logic.AuthLogic): 认证领域操作。
//   - authService (*AuthService): 认证应用服务（用于强退登记）。
//
// 返回 Returns:
//   - service (*OnlineService): 在线用户应用服务。
func NewOnlineService(auth *logic.AuthLogic, authService *AuthService) *OnlineService {
	return &OnlineService{auth: auth, auths: authService}
}

// Page 分页查询在线用户。
//
// 参数 Parameters:
//   - q (dto.OnlineQuery): 查询条件。
//
// 返回 Returns:
//   - result (dto.PageResult[dto.OnlineUser]): 分页结果。
//   - err (error): 查询失败时返回业务错误。
func (s *OnlineService) Page(q dto.OnlineQuery) (dto.PageResult[dto.OnlineUser], error) {
	page, size := util.NormalizePage(q.PageNum, q.PageSize)
	rows, total, err := s.auth.ActiveSessions(q.Keywords, q.Username, q.IP, util.Offset(page, size), size)
	if err != nil {
		return dto.PageResult[dto.OnlineUser]{}, err
	}
	list := make([]dto.OnlineUser, 0, len(rows))
	for index := range rows {
		list = append(list, newOnlineUser(&rows[index]))
	}
	return dto.PageResult[dto.OnlineUser]{List: list, Total: total}, nil
}

// Overview 返回在线概览（会话数与去重用户数）。
//
// 返回 Returns:
//   - overview (dto.OnlineOverview): 概览数据。
//   - err (error): 查询失败时返回业务错误。
func (s *OnlineService) Overview() (dto.OnlineOverview, error) {
	rows, _, err := s.auth.ActiveSessions("", "", "", 0, 1000)
	if err != nil {
		return dto.OnlineOverview{}, err
	}
	users := make(map[int64]bool, len(rows))
	for index := range rows {
		users[rows[index].UserID] = true
	}
	return dto.OnlineOverview{Total: int64(len(rows)), UserTotal: int64(len(users))}, nil
}

// Kick 强制下线指定会话（吊销刷新令牌并登记 access token 失效标记）。
//
// 参数 Parameters:
//   - ctx (context.Context): 请求上下文。
//   - idText (string): 会话编号。
//   - operator (*model.SysUser): 当前操作人，用于审计日志的用户名。
//
// 返回 Returns:
//   - err (error): 会话不存在或写入失败时返回业务错误。
func (s *OnlineService) Kick(ctx context.Context, idText string, operator *model.SysUser) error {
	sessionID := strings.TrimSpace(idText)
	if sessionID == "" {
		return apperr.Invalid("会话编号不能为空")
	}
	row, err := s.auth.SessionByID(sessionID)
	if err != nil {
		return err
	}
	if row.RevokedAt != nil {
		return apperr.Conflict("该会话已下线")
	}
	if err := s.auth.RevokeSession(sessionID); err != nil {
		return err
	}
	// 让已签发的 access token 立即失效（TTL 取访问令牌有效期）。
	s.auths.RevokeSessionID(ctx, sessionID)
	// 记一条登录日志，便于审计「谁被强制下线」。
	operatorName := "system"
	if operator != nil && strings.TrimSpace(operator.Username) != "" {
		operatorName = operator.Username
	}
	record := logic.BuildLoginLog(logic.LoginLog{
		Username:   row.Username,
		IP:         util.TextValue(row.IP),
		IPLocation: util.TextValue(row.IPLocation),
		OS:         util.TextValue(row.OS),
		Browser:    util.TextValue(row.Browser),
		Status:     LoginStatusFailed,
		Message:    "强制下线（操作人 " + operatorName + "）",
	})
	if err := s.auth.AddLoginLog(record); err != nil {
		// 审计日志失败不影响强退结果。
		return nil
	}
	return nil
}

// newOnlineUser 把会话行转换为前端列表项。
//
// 参数 Parameters:
//   - row (*logic.Session): 会话行。
//
// 返回 Returns:
//   - item (dto.OnlineUser): 列表项。
func newOnlineUser(row *logic.Session) dto.OnlineUser {
	deptName := ""
	if row.DeptName != nil {
		deptName = *row.DeptName
	}
	lastActive := row.LastActiveAt
	if lastActive == nil {
		lastActive = row.CreatedAt
	}
	return dto.OnlineUser{
		ID:             row.ID,
		Username:       row.Username,
		Nickname:       row.Nickname,
		DeptName:       deptName,
		IP:             util.TextValue(row.IP),
		IPLocation:     util.TextValue(row.IPLocation),
		Device:         util.TextValue(row.Device),
		OS:             util.TextValue(row.OS),
		Browser:        util.TextValue(row.Browser),
		LoginTime:      util.TimeText(row.CreatedAt),
		LastActiveTime: util.TimeText(lastActive),
		ExpiresTime:    util.TimeValueText(row.ExpiresAt),
	}
}

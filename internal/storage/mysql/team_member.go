package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/zloymelnik/task-tracker/internal/domain"
)

type TeamMemberRepository struct {
	db      *sqlx.DB
	log     *slog.Logger
	timeout time.Duration
}

func NewTeamMemberRepository(db *sqlx.DB, log *slog.Logger, timeout time.Duration) *TeamMemberRepository {
	return &TeamMemberRepository{
		db:      db,
		log:     log,
		timeout: timeout,
	}
}

func (t *TeamMemberRepository) Save(ctx context.Context, team *domain.TeamMember) error {
	t.log.Debug("MySQL: Team Member: Save team member data", "team_id", team.TeamID, "user_id", team.UserID, "role", team.Role)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	_, err := executor.ExecContext(ctx,
		"INSERT INTO team_members (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?)",
		uuidToBytes(team.TeamID), uuidToBytes(team.UserID), team.Role, team.JoinedAt,
	)
	if err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return domain.ErrTeamMemberAlreadyExists
		}
		return fmt.Errorf("MySQL: Team Member: Failed to save team member data: %w", err)
	}
	t.log.Debug("MySQL: Team Member: Team member data saved successfully")
	return nil
}

type rawTeamMember struct {
	TeamID   []byte    `db:"team_id"`
	UserID   []byte    `db:"user_id"`
	Role     string    `db:"role"`
	JoinedAt time.Time `db:"joined_at"`
}

func (t *TeamMemberRepository) GetByTeamAndUserID(
	ctx context.Context, teamID, userID uuid.UUID,
) (*domain.TeamMember, error) {
	return t.getByTeamAndUserID(ctx, teamID, userID, false)
}

func (t *TeamMemberRepository) GetByTeamAndUserIDForUpdate(
	ctx context.Context, teamID, userID uuid.UUID,
) (*domain.TeamMember, error) {
	return t.getByTeamAndUserID(ctx, teamID, userID, true)
}

func (t *TeamMemberRepository) getByTeamAndUserID(
	ctx context.Context,
	teamID uuid.UUID,
	userID uuid.UUID,
	needBlock bool,
) (*domain.TeamMember, error) {
	t.log.Debug("MySQL: Team Member: Gets member data by user_id and team_id", "team_id", teamID, "user_id", userID)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)

	var raw rawTeamMember
	query := `SELECT team_id, user_id, role, joined_at FROM team_members WHERE team_id = ? AND user_id = ?`
	if needBlock {
		tx, ok := ctx.Value(txKey{}).(*sqlx.Tx)
		if !ok || tx == nil {
			return nil, fmt.Errorf("MySQL: Team Member: get team member for update: transaction required")
		}
		query += " FOR UPDATE"
	}
	err := executor.GetContext(ctx, &raw, query, uuidToBytes(teamID), uuidToBytes(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrTeamMemberNotFound
		}
		return nil, fmt.Errorf("MySQL: Team Member: Failed to get team member row: %w", err)
	}
	teamMember, err := rawToTeamMember(&raw)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team Member: Failed to parse team member row: %w", err)
	}
	t.log.Debug("MySQL: Team Member: Gets team member data for user_id and team_id successfully", "user_id", userID, "team_id", teamID)
	return teamMember, nil
}

func rawToTeamMember(r *rawTeamMember) (*domain.TeamMember, error) {
	userID, err := bytesToUUID(r.UserID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team Member: Wrong parse user_id: %w", err)
	}
	teamID, err := bytesToUUID(r.TeamID)
	if err != nil {
		return nil, fmt.Errorf("MySQL: Team Member: Wrong parse team_id: %w", err)
	}
	return &domain.TeamMember{
		TeamID:   teamID,
		UserID:   userID,
		Role:     domain.TeamRole(r.Role),
		JoinedAt: r.JoinedAt,
	}, nil
}

func (t *TeamMemberRepository) UpdateRole(
	ctx context.Context,
	teamID uuid.UUID,
	userID uuid.UUID,
	role domain.TeamRole,
) error {
	t.log.Debug("MySQL: Team Member: Change member role", "team_id", teamID, "user_id", userID, "role", role)
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	executor := getExecutor(ctx, t.db)
	_, err := executor.ExecContext(ctx,
		"UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?",
		role, uuidToBytes(teamID), uuidToBytes(userID),
	)
	if err != nil {
		return fmt.Errorf("MySQL: Team Member: Failed to change role: %w", err)
	}
	t.log.Debug("MySQL: Team Member: Change member role sucessfully", "team_id", teamID, "user_id", userID, "role", role)
	return nil
}

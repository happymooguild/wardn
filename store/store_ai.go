package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// --- AI provider configuration -------------------------------------------

const aiProviderCols = `id, kind, model, base_url, key_last4, enabled, created_at, updated_at`

func scanAIProvider(scanner interface {
	Scan(dest ...any) error
}) (AIProvider, error) {
	var p AIProvider
	err := scanner.Scan(&p.ID, &p.Kind, &p.Model, &p.BaseURL, &p.KeyLast4,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// EnabledAIProvider returns the active provider's metadata. The API key is
// deliberately not included - use AIProviderKey when you need to make a call.
func (s *Store) EnabledAIProvider(ctx context.Context) (AIProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+aiProviderCols+` FROM ai_providers WHERE enabled ORDER BY updated_at DESC LIMIT 1`)
	return scanAIProvider(row)
}

// AIProviderKey returns the encrypted key blob for the active provider.
func (s *Store) AIProviderKey(ctx context.Context, id int64) ([]byte, error) {
	var enc []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT api_key_enc FROM ai_providers WHERE id = $1`, id).Scan(&enc)
	return enc, err
}

// UpsertAIProvider replaces the active provider. There is exactly one at a
// time, so this disables any previous row inside the same transaction rather
// than accumulating stale credentials.
func (s *Store) UpsertAIProvider(ctx context.Context, kind, model, baseURL string, keyEnc []byte, keyLast4 string) (AIProvider, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIProvider{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_providers`); err != nil {
		return AIProvider{}, err
	}

	row := tx.QueryRowContext(ctx,
		`INSERT INTO ai_providers (kind, model, base_url, api_key_enc, key_last4, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)
		 RETURNING `+aiProviderCols,
		kind, model, baseURL, keyEnc, keyLast4)
	p, err := scanAIProvider(row)
	if err != nil {
		return AIProvider{}, err
	}
	return p, tx.Commit()
}

func (s *Store) DeleteAIProvider(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ai_providers`)
	return err
}

// SetAppAIEnabled toggles automatic analysis for one app.
func (s *Store) SetAppAIEnabled(ctx context.Context, appID int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE apps SET ai_enabled = $2 WHERE id = $1`, appID, enabled)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Analyses -------------------------------------------------------------

const analysisCols = `id, deploy_event_id, status, trigger_source, provider, model, summary,
	likely_cause, confidence, evidence, suggested_steps, context_stats,
	input_tokens, output_tokens, error_message, created_at, completed_at`

func scanAnalysis(scanner interface {
	Scan(dest ...any) error
}) (Analysis, error) {
	var a Analysis
	err := scanner.Scan(&a.ID, &a.DeployEventID, &a.Status, &a.Trigger, &a.Provider, &a.Model,
		&a.Summary, &a.LikelyCause, &a.Confidence, &a.Evidence, &a.SuggestedSteps,
		&a.ContextStats, &a.InputTokens, &a.OutputTokens, &a.Error, &a.CreatedAt, &a.CompletedAt)
	return a, err
}

// CreateAnalysis records a pending analysis and enqueues the job that will
// fulfil it, in one transaction - so a row is never left pending with no job
// to advance it.
func (s *Store) CreateAnalysis(ctx context.Context, deployID int64, trigger string) (Analysis, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Analysis{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	row := tx.QueryRowContext(ctx,
		`INSERT INTO analyses (deploy_event_id, status, trigger_source)
		 VALUES ($1, 'pending', $2)
		 RETURNING `+analysisCols,
		deployID, trigger)
	a, err := scanAnalysis(row)
	if err != nil {
		return Analysis{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO analysis_jobs (deploy_event_id, kind, run_after) VALUES ($1, $2, now())`,
		deployID, JobKindAI); err != nil {
		return Analysis{}, err
	}

	return a, tx.Commit()
}

// PendingAnalysis returns the oldest not-yet-finished analysis for a deploy.
// The AI worker uses it to find the row its job corresponds to.
func (s *Store) PendingAnalysis(ctx context.Context, deployID int64) (Analysis, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+analysisCols+` FROM analyses
		  WHERE deploy_event_id = $1 AND status IN ('pending','running')
		  ORDER BY created_at
		  LIMIT 1`, deployID)
	return scanAnalysis(row)
}

// HasOpenAnalysis reports whether an analysis is already queued or running, so
// repeated "Ask AI" clicks don't fan out into duplicate model calls.
func (s *Store) HasOpenAnalysis(ctx context.Context, deployID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM analyses
		  WHERE deploy_event_id = $1 AND status IN ('pending','running')`, deployID).Scan(&n)
	return n > 0, err
}

func (s *Store) GetAnalysis(ctx context.Context, id int64) (Analysis, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+analysisCols+` FROM analyses WHERE id = $1`, id)
	return scanAnalysis(row)
}

func (s *Store) ListAnalyses(ctx context.Context, deployID int64) ([]Analysis, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+analysisCols+` FROM analyses WHERE deploy_event_id = $1 ORDER BY created_at DESC`,
		deployID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Analysis, 0)
	for rows.Next() {
		a, err := scanAnalysis(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LatestAnalysis returns the most recent analysis for a deploy, so the deploy
// detail endpoint can render in a single round-trip.
func (s *Store) LatestAnalysis(ctx context.Context, deployID int64) (Analysis, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+analysisCols+` FROM analyses
		  WHERE deploy_event_id = $1 ORDER BY created_at DESC LIMIT 1`, deployID)
	return scanAnalysis(row)
}

func (s *Store) MarkAnalysisRunning(ctx context.Context, id int64, provider, model string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE analyses SET status = 'running', provider = $2, model = $3 WHERE id = $1`,
		id, provider, model)
	return err
}

// AnalysisResult is the successful outcome written back by the worker.
type AnalysisResult struct {
	Provider     string
	Model        string
	Summary      string
	LikelyCause  string
	Confidence   string
	Evidence     []string
	Suggested    []string
	ContextStats any
	InputTokens  int
	OutputTokens int
}

func (s *Store) CompleteAnalysis(ctx context.Context, id int64, r AnalysisResult) error {
	evidence, err := json.Marshal(r.Evidence)
	if err != nil {
		return err
	}
	steps, err := json.Marshal(r.Suggested)
	if err != nil {
		return err
	}
	stats, err := json.Marshal(r.ContextStats)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE analyses
		    SET status = 'succeeded', provider = $2, model = $3, summary = $4,
		        likely_cause = $5, confidence = $6, evidence = $7, suggested_steps = $8,
		        context_stats = $9, input_tokens = $10, output_tokens = $11,
		        error_message = NULL, completed_at = now()
		  WHERE id = $1`,
		id, r.Provider, r.Model, r.Summary, r.LikelyCause, r.Confidence,
		evidence, steps, stats, r.InputTokens, r.OutputTokens)
	return err
}

// FailAnalysis records a terminal failure. status is 'failed' for an error the
// operator can fix, or 'refused' when the model's safety layer declined -
// which is not retryable and shouldn't read as a bug.
func (s *Store) FailAnalysis(ctx context.Context, id int64, status, message string, contextStats any) error {
	stats, err := json.Marshal(contextStats)
	if err != nil {
		stats = []byte(`{}`)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE analyses SET status = $2, error_message = $3, context_stats = $4, completed_at = now()
		  WHERE id = $1`,
		id, status, message, stats)
	return err
}

// StaleAnalyses fails analyses left pending by a worker that died mid-flight,
// so the UI stops polling a row nothing will ever advance.
func (s *Store) StaleAnalyses(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := s.db.ExecContext(ctx,
		`UPDATE analyses
		    SET status = 'failed', error_message = 'analysis timed out', completed_at = now()
		  WHERE status IN ('pending','running') AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

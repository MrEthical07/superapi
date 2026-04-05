package storage

import "context"

type relationalExecOperation struct {
	query string
	args  []any
}

// RelationalExec builds an execution-only operation for write statements.
func RelationalExec(query string, args ...any) RelationalOperation {
	copied := append([]any(nil), args...)
	return relationalExecOperation{query: query, args: copied}
}

func (o relationalExecOperation) ExecuteRelational(ctx context.Context, exec RelationalExecutor) error {
	return exec.Exec(ctx, o.query, o.args...)
}

type relationalQueryOneOperation struct {
	query string
	args  []any
	scan  func(RowScanner) error
}

// RelationalQueryOne builds an operation that scans exactly one row.
func RelationalQueryOne(query string, scan func(RowScanner) error, args ...any) RelationalOperation {
	copied := append([]any(nil), args...)
	return relationalQueryOneOperation{query: query, args: copied, scan: scan}
}

func (o relationalQueryOneOperation) ExecuteRelational(ctx context.Context, exec RelationalExecutor) error {
	return exec.QueryRow(ctx, o.query, o.scan, o.args...)
}

type relationalQueryManyOperation struct {
	query string
	args  []any
	scan  func(RowScanner) error
}

// RelationalQueryMany builds an operation for scanning many rows.
func RelationalQueryMany(query string, scan func(RowScanner) error, args ...any) RelationalOperation {
	copied := append([]any(nil), args...)
	return relationalQueryManyOperation{query: query, args: copied, scan: scan}
}

func (o relationalQueryManyOperation) ExecuteRelational(ctx context.Context, exec RelationalExecutor) error {
	return exec.Query(ctx, o.query, o.scan, o.args...)
}

type documentRunOperation struct {
	command string
	payload any
	out     any
}

// DocumentRun builds an execution-only document operation.
func DocumentRun(command string, payload any, out any) DocumentOperation {
	return documentRunOperation{command: command, payload: payload, out: out}
}

func (o documentRunOperation) ExecuteDocument(ctx context.Context, exec DocumentExecutor) error {
	return exec.Run(ctx, o.command, o.payload, o.out)
}

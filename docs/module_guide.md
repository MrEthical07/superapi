# Module Data Layer Guide

This guide focuses on one thing: how to implement module data access correctly under the store-first architecture.

If you only need practical rules for service/repository/store code, this is the right document.

## 1. Non-Negotiable Architecture Rules

Required flow:

Service -> Repository -> Store -> Backend

Hard constraints:

- service calls repository only
- repository calls store only
- handler does not bypass service/repository
- one storage type per module

Why this exists:

- prevents architecture drift over time
- keeps business code backend-agnostic
- allows backend implementation changes with lower blast radius

## 2. Storage Contracts You Should Know

Storage contracts are in internal/core/storage/contracts.go.

Core interfaces:

- Store
	- Kind() Kind
- TransactionalStore
	- WithTx(ctx, fn)
- RelationalStore
	- Execute(ctx, RelationalOperation)
- DocumentStore
	- Execute(ctx, DocumentOperation)

Key idea:

- stores execute operations
- repositories define operations

## 3. Choosing A Storage Type Per Module

At module wiring time, choose one backend family:

- relational module -> RelationalStore
- document module -> DocumentStore

Do not branch inside business flow with "if sql else document".

If you need both for a feature, split into separate modules with explicit boundaries.

## 4. Service Layer Pattern

Service responsibilities:

- validate business-level input and rules
- orchestrate sequence of repository calls
- control write transaction boundaries

Service should not:

- construct queries
- call store execution methods directly
- depend on driver/query-object types

### 4.1 Write path service skeleton

Pattern:

1. validate request
2. start store.WithTx
3. call repository write methods inside callback
4. return domain output

### 4.2 Read path service skeleton

Pattern:

1. validate request
2. call repository read method directly
3. return domain output

No transaction wrapper unless you have a specific reason.

## 5. Repository Layer Pattern

Repository responsibilities:

- own query/filter/projection logic
- translate domain inputs into storage operations
- map row/document results to domain models
- map backend/storage errors to domain/app errors where needed

Repository should not:

- orchestrate high-level business workflows
- expose backend-specific types in public interfaces

### 5.1 Relational repository pattern

Use operation helpers from internal/core/storage/operations.go:

- RelationalExec
- RelationalQueryOne
- RelationalQueryMany

Repository creates operation with query and scan callback, then calls store.Execute.

### 5.2 Document repository pattern

Use DocumentRun to execute command/payload patterns via DocumentStore.

Keep domain mapping in repository, not in store executor.

## 6. Transaction Rules (Detailed)

Transaction API exists at store layer for all backends.

Rules:

- write paths use store.WithTx
- read paths are direct repository calls by default
- repository methods should be transaction-context aware through context propagation
- backend-specific commit/rollback behavior remains store concern

## 7. Interface Design Rules

Good repository interface:

- domain nouns and verbs
- clear use-case semantics
- no backend leakage

Examples:

- CreateOrder(ctx, input) (Order, error)
- GetOrderByID(ctx, tenantID, orderID) (Order, error)
- ListOrders(ctx, tenantID, filter) ([]Order, error)

Bad examples:

- ExecSQL(ctx, query, args...)
- QueryRows(ctx, stmt) (...)
- Find(ctx, bson.M)

## 8. Mapping Rules

Repository mapping direction:

- domain input -> storage payload/query args
- storage row/doc -> domain output

Store layer remains domain-agnostic and execution-focused.

This rule keeps storage changes local to repository/store implementation.

## 9. Using The Module Scaffold Safely

The generator gives a fast starter layout.

Before shipping:

- adjust generated service/repo contracts to domain-focused signatures
- ensure service does not drift into store/driver calls
- implement real repository operations and mappings
- add tests for read and write behaviors

## 10. Validation Checklist Before PR

1. Service file contains no direct store or driver calls.
2. Repository interface uses domain methods only.
3. Repository implementation owns query/filter logic.
4. Store implementation contains no domain structures.
5. Write paths are wrapped in transaction flow.
6. Read paths avoid unnecessary transaction wrappers.
7. Policy stacks are valid for protected routes.
8. go test ./..., go build ./..., and make verify pass.

## 11. Quick Anti-Pattern Table

| Anti-pattern | Why it is bad | Correct replacement |
|---|---|---|
| Service directly executes SQL | Breaks architecture boundary | Move query code to repository |
| Repository returns driver rows | Leaks backend details upward | Return domain models |
| Handler performs business decisions | Hard to test and reuse | Move to service |
| One module switches between SQL/doc backends | Increases branch complexity and risk | Separate module or explicit backend choice |

## 12. Related References

- [docs/modules.md](modules.md)
- [docs/crud-examples.md](crud-examples.md)
- [docs/architecture.md](architecture.md)
- [docs/policies.md](policies.md)

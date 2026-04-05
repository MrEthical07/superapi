# Module Author Guide

This guide explains how to create and maintain modules in SuperAPI.

If you are new to this repo, read this guide before writing a new feature module.

## 1. What Is A Module?

A module is a feature package under internal/modules.

It owns:

- its routes
- its handler/service/repository code
- its DTOs
- its tests

The module implements the app.Module interface and optionally app.DependencyBinder.

## 2. Standard Module Layout

Typical layout:

internal/modules/projects/
- module.go
- routes.go
- dto.go
- handler.go
- service.go
- repo.go
- handler_test.go
- service_test.go

What each file should do:

- module.go
	- module struct
	- Name()
	- BindDependencies(...)
	- constructor wiring
- routes.go
	- Register(...)
	- route to handler mapping
	- policy stack attachment
- dto.go
	- request/response DTOs
	- DTO validation helpers
- handler.go
	- transport logic only
	- parse path/query/input context
	- call service
- service.go
	- business workflows
	- transaction boundary decisions for write flows
- repo.go
	- query/filter/projection logic
	- mapping between storage model and domain model

## 3. The Required Data Flow

Every module must follow:

Service -> Repository -> Store -> Backend

Do not bypass this flow.

Forbidden patterns:

- handler calling store directly
- service calling store directly
- service calling driver directly
- repository calling pgx/redis/document driver directly

## 4. Layer Responsibilities (Practical Version)

### 4.1 Handler layer

Handler should:

- read request input
- call service
- return response DTO

Handler should not:

- run business workflows
- open transactions
- run data-access queries

### 4.2 Service layer

Service should:

- validate business rules
- orchestrate use-cases
- choose transaction boundary for write paths

Service should not:

- hold backend-specific query code
- expose backend-specific types in interfaces

### 4.3 Repository layer

Repository should:

- implement domain-focused repository methods
- own all query/filter logic
- map storage structures to domain model

Repository should not:

- include unrelated business workflow decisions
- leak backend query objects into public contracts

### 4.4 Store layer

Store should:

- execute repository-defined operations
- provide transaction behavior via WithTx

Store should not:

- know module domain types
- encode module-specific semantics

## 5. Dependency Injection In Modules

Modules receive dependencies via BindDependencies.

Use module runtime surface from internal/core/modulekit/runtime.go.

Available store accessors:

- Store()
- RelationalStore()
- DocumentStore()

Important rule:

- each module chooses one storage type and wires repository with that backend

## 6. Route Registration And Policy Order

Routes are registered in routes.go through router.Handle(...).

Policy order must follow:

1. auth
2. tenant
3. rbac
4. rate limit
5. cache
6. cache-control

Why this matters:

- policy validator enforces order and dependency safety
- invalid policy stacks fail fast

## 7. Service Patterns: Read vs Write

Read path pattern:

- handler -> service -> repository -> store.Execute(read operation)
- no transaction wrapper by default

Write path pattern:

- handler -> service -> store.WithTx(...) -> repository write methods -> store.Execute(write operations)

Service owns workflow boundary; repository owns storage operations.

## 8. Repository Contract Design

Repository interfaces should use domain terms.

Good contract examples:

- CreateProject(ctx, input) (Project, error)
- GetProjectByID(ctx, tenantID, id) (Project, error)
- ListProjects(ctx, tenantID, limit) ([]Project, error)

Bad contract examples:

- ExecuteSQL(ctx, query, args...)
- Find(ctx, map[string]any)
- methods that return driver-specific row/query types

## 9. About The Module Scaffold Output

The module generator provides a fast skeleton to start coding.

Treat generated code as a baseline, not as final architecture-complete business code.

After generating a module, verify and refine:

- update service/repository contracts to domain-focused shape
- ensure service -> repository -> store flow is respected
- add route policies in correct order
- add tests for use-cases and policy-protected routes

## 10. Testing Strategy For Modules

Minimum recommended tests:

- DTO validation tests
- handler tests for input/output shape
- service tests for business workflows
- repository tests for mapping/error behavior

Common checks before merge:

- go test ./...
- go build ./...
- make verify

## 11. Common Mistakes And Fixes

Mistake: service imports driver packages

- Fix: move backend logic into repository/store operation code

Mistake: handler validates business rules deeply

- Fix: keep handler transport-level; move business decisions to service

Mistake: repository interface leaks storage terms

- Fix: use domain nouns and use-case method names

Mistake: authenticated cache route has no user/tenant vary key

- Fix: configure safe vary dimensions so policy validation and runtime isolation stay correct

## 12. Module Completion Checklist

1. module.go wires dependencies and runtime correctly.
2. routes.go policy order is correct and explicit.
3. handler.go contains transport-only logic.
4. service.go contains business orchestration only.
5. repo.go owns query logic and model mapping.
6. write flows use transaction path.
7. read flows avoid unnecessary transaction wrappers.
8. interfaces expose domain-focused methods only.
9. tests cover happy path and common failures.
10. verify/build/test commands pass.

## 13. Next Reading

- [docs/module_guide.md](module_guide.md)
- [docs/crud-examples.md](crud-examples.md)
- [docs/policies.md](policies.md)
- [docs/cache-guide.md](cache-guide.md)

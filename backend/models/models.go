// Package models contains the data types used throughout portfolio-analysis.
//
// Types are now split across three files:
//   - domain.go  — core domain types (Trade, FlexQueryData, AccountingModel, …)
//   - api.go     — HTTP response structs (PositionValue, StatsResponse, …)
//   - db.go      — GORM ORM models (Transaction, MarketData, …)
package models

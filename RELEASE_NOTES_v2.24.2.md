# PairProxy v2.24.2 Release Notes

**Release Date**: 2026-04-07  
**Tag**: `v2.24.2`

---

## 🎯 Overview

v2.24.2 is a **feature release** that extends the reportgen tool with full PostgreSQL database support while maintaining complete backward compatibility with existing SQLite workflows.

---

## ✨ Key Features

### Reportgen: PostgreSQL Database Support

The reportgen analysis tool now supports both **SQLite** and **PostgreSQL** databases, making it suitable for production environments using PostgreSQL as their primary database.

#### Connection Options

**1. SQLite (Original - No Changes Required)**
```bash
./reportgen -db pairproxy.db -from 2026-04-01 -to 2026-04-07
```

**2. PostgreSQL with Full DSN**
```bash
./reportgen -pg-dsn "postgres://user:password@host:5432/dbname" -from 2026-04-01 -to 2026-04-07
```

**3. PostgreSQL with Individual Parameters**
```bash
./reportgen -pg-host localhost -pg-user app -pg-password secret \
  -pg-dbname pairproxy -pg-port 5432 -pg-sslmode disable \
  -from 2026-04-01 -to 2026-04-07
```

#### Technical Implementation

**Database Abstraction Layer**
- Unified `Querier` struct handles both SQLite and PostgreSQL
- Automatic SQL dialect conversion:
  - Parameter binding: `?` → `$1, $2, ...` (PostgreSQL)
  - Date functions: `DATE()` ↔ `TO_CHAR(..., 'YYYY-MM-DD')`
  - Hour extraction: `strftime('%H', col)` ↔ `EXTRACT(HOUR FROM col)`
  - Day-of-week: `strftime('%w', col)` ↔ `EXTRACT(DOW FROM col)`

**Driver Support**
- SQLite: `modernc.org/sqlite` (unchanged)
- PostgreSQL: `github.com/lib/pq` (newly added)

**SQLite Optimizations** (Unchanged)
- WAL (Write-Ahead Logging) mode for improved concurrency
- Connection pooling with sensible defaults

---

## 📋 Changes

### Code Changes
- **main.go**: Added PostgreSQL connection flags and driver detection logic
- **queries.go**: Implemented `Querier` abstraction with `rebind()` and SQL dialect helpers
- **queries_phase3.go, phase4.go, phase6.go, phase8.go**: Updated to use dialect-aware helper methods
- **generator.go**: Updated to pass driver and DSN to report generation pipeline
- **insights_llm.go**: Enhanced `QueryLLMTarget()` to support PostgreSQL connections
- **types.go**: Extended `QueryParams` with `Driver` and `DSN` fields
- **go.mod**: Added `github.com/lib/pq v1.10.9` dependency

### Documentation Updates
- **tools/reportgen/README.md**: 
  - Updated overview with PostgreSQL support
  - Expanded command-line reference with 3 connection modes
  - Added PostgreSQL-specific usage examples
  - SSL mode configuration details

- **docs/CHANGELOG.md**: Added v2.24.2 entry with feature summary

---

## ✅ Compatibility

### Backward Compatibility
✅ **100% Backward Compatible**
- Existing SQLite workflows require **zero changes**
- Original `-db` flag continues to work as before
- No breaking changes to report format or functionality

### Platform Support
All platforms receive cross-compiled reportgen binaries:
- Linux x86_64, ARM64
- macOS x86_64, ARM64 (Apple Silicon)
- Windows x86_64, ARM64

Download from [GitHub Releases](https://github.com/l17728/pairproxy/releases/tag/v2.24.2)

---

## 🔍 Testing

### Validation Performed
- ✅ Code compilation on all platforms
- ✅ Help text and flag parsing
- ✅ SQLite backward compatibility
- ✅ PostgreSQL connection string parsing (DSN mode)
- ✅ PostgreSQL individual parameter mode
- ✅ SQL dialect conversion accuracy

### Known Limitations
- PostgreSQL connection pooling uses default `database/sql` settings (can be tuned via future config)
- LLM insights feature works with both databases seamlessly

---

## 📦 Installation

### From Pre-built Binaries
Download from [GitHub Releases](https://github.com/l17728/pairproxy/releases/tag/v2.24.2):
```bash
tar -xzf reportgen-v2.24.2-linux-amd64.tar.gz
./reportgen -help
```

### From Source
```bash
cd tools/reportgen
go build -o reportgen
./reportgen -help
```

---

## 🚀 Upgrade Path

**No action required** for existing users.

**To use PostgreSQL**: Simply switch to using `-pg-dsn` or `-pg-*` flags:
```bash
# Old (SQLite)
./reportgen -db pairproxy.db -from 2026-04-01 -to 2026-04-07

# New (PostgreSQL)
./reportgen -pg-dsn "postgres://user:pass@db.example.com:5432/pairproxy" \
  -from 2026-04-01 -to 2026-04-07
```

---

## 🐛 Bug Fixes
None (feature release)

---

## 📝 Known Issues
None at this time.

---

## 👥 Contributors
- Claude Haiku 4.5 (AI Assistant) - PostgreSQL support implementation

---

## 📚 Additional Resources
- [Reportgen README](tools/reportgen/README.md) - Full usage documentation
- [Database Configuration](docs/DATABASE.md) - Database layer details
- [GitHub Repository](https://github.com/l17728/pairproxy)

---

**Generated**: 2026-04-07  
**Next Release**: v2.25.0 (planned enhancements)

# Dinero - Personal Finance Search Skill

You have access to the `dinero` CLI to search personal financial transactions stored in a database.

## Setup

Before searching, ensure the cache is up to date by running:

```bash
dinero build-cache
```

This creates two files in `~/.config/dinero/cache/`:

- **`accounts.json`** -- list of all account names
- **`categories.json`** -- all categories, subcategories, and their pairings

Read these files first to know the valid values for filters. Always use exact account/category names from the cache when constructing queries.

## Searching Transactions

Use the `dinero search` command with any combination of the following filters:

```
dinero search [OPTIONS]
```

### Filters

| Flag | Type | Description |
|------|------|-------------|
| `--account` | string | Filter by account name (exact match) |
| `--category` | string | Filter by category (exact match) |
| `--subcategory` | string | Filter by subcategory (exact match) |
| `--description` | string | Search in description (case-insensitive partial match) |
| `--after` | string | Transactions on or after this date (YYYY-MM-DD) |
| `--before` | string | Transactions on or before this date (YYYY-MM-DD) |
| `--year` | int | Filter by year |
| `--month` | int | Filter by month (1-12) |
| `--limit` | int | Max rows returned (default: 50) |
| `--sort` | string | Sort by column: date, amount, description, account, category (default: date) |
| `--desc` / `--asc` | flag | Sort direction (default: `--desc`, most recent first) |
| `--json` | flag | Output as JSON instead of table |

### Important Notes

- All filters are optional and can be combined
- Account and category names must be exact matches -- always check the cache files first
- Description search is a case-insensitive partial match (e.g., `--description "walmart"` matches "WALMART SUPERCENTER #1234")
- Dates must be in `YYYY-MM-DD` format
- Default sort is by date descending (most recent first)
- Default limit is 50 rows. Increase with `--limit` if needed.

## Workflow

When asked about transactions, follow this process:

1. **Read the cache files** to get valid account names, categories, and subcategories:
   - Read `~/.config/dinero/cache/accounts.json`
   - Read `~/.config/dinero/cache/categories.json`
   - If these files don't exist, run `dinero build-cache` first

2. **Construct the search query** using the appropriate filters based on the user's request

3. **Execute the search** and present the results

## Examples

### Get the last 10 transactions from a specific account
```bash
dinero search --account "Chase Checking" --limit 10
```

### Find all grocery transactions this year
```bash
dinero search --category "Food & Drink" --subcategory "Groceries" --year 2025
```

### Search for a specific merchant
```bash
dinero search --description "netflix"
```

### Get transactions in a date range
```bash
dinero search --after "2025-01-01" --before "2025-01-31"
```

### Get the largest transactions from last month as JSON
```bash
dinero search --year 2025 --month 12 --sort amount --json
```

### Combine multiple filters
```bash
dinero search --account "Chase Checking" --category "Shopping" --after "2025-06-01" --limit 20
```

---
description: Parse a PDF statement and extract transactions to CSV
---

Parse the PDF statement at `$1` and extract transactions to a CSV file.

## Instructions

1. Extract PDF content by running:

```bash
uv run python - "$1" <<'PYTHON'
import pdfplumber
import sys

pdf_path = sys.argv[1]

with pdfplumber.open(pdf_path) as pdf:
    for i, page in enumerate(pdf.pages):
        print(f"\n{'='*80}")
        print(f"PAGE {i+1}")
        print('='*80)

        # Extract text
        text = page.extract_text()
        if text:
            print("\n--- TEXT ---")
            print(text)

        # Extract tables
        tables = page.extract_tables()
        if tables:
            for j, table in enumerate(tables):
                print(f"\n--- TABLE {j+1} ---")
                for row in table:
                    print(" | ".join(str(cell) if cell else "" for cell in row))
PYTHON
```

2. Analyze the extracted content to identify transactions or summary items

3. Determine statement type:
   - **Detailed transactions**: Bank/credit card statements with individual transactions per row (use transaction date for each row)
   - **Summary statements**: Brokerage statements where aggregate items from the Account Summary section should each become one transaction

4. For **brokerage summary statements**, extract ALL of the following items from the Account Summary (including zeros):
   - Deposits
   - Withdrawals
   - Dividends and Interest
   - Transfer of Securities
   - Market Appreciation/(Depreciation)
   - Expenses
   
   Use the **last day of the statement period** as the date for all summary items.

5. Ask the user which items to extract if the statement type is unclear

6. Generate a CSV file in the same directory as the PDF with `.csv` extension

## CSV Format

Required columns: `date`, `description`, `amount`
Optional columns: `category`, `subcategory`

```csv
date,description,amount
2026-01-31,Deposits,-5000.00
2026-01-31,Withdrawals,1200.00
```

## Amount Sign Convention (Plaid convention)

- **Positive** = money OUT (debits, expenses, withdrawals)
- **Negative** = money IN (credits, deposits, income)

## Date Handling

- Use ISO format: `YYYY-MM-DD`
- For summary statements, use the **last day of the statement period**

## Output

Save CSV as: `<pdf-filename>.csv` (same directory, same base name)

Example:

- Input: `Statement_022026_7154.pdf`
- Output: `Statement_022026_7154.csv`

## After Completion

Tell the user they can import with:

```bash
dinero import-csv ./<output>.csv "Account Name"
```

Do NOT run the import command — let the user do it after reviewing the CSV.

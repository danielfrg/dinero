# dinero

Tools and scripts to manage my personal finances.
Your own Mint/YNAB, self hosted.

- [Plaid](https://plaid.com) for collecting transactions from financial institutions
- [NocoDB](https://nocodb.com) for storing transactions and making nice views of them
  - I use this instead of Google Sheets because it allows me to easily group by
    Account, Category and more
  - Initially I used Airtable but the new pricing made it not worth it

Inspired by [yyx990803/build-your-own-mint](https://github.com/yyx990803/build-your-own-mint).

## Usage

Get new transactions and add them to the database:

```terminal
task transactions
```

Example output:

```terminal
task: [transactions] python scripts/transactions.py
2023-09-21 11:06.45 [info     ] Transactions downloaded        name=XXX records=49
2023-09-21 11:06.45 [info     ] Transactions pending           name=XXX records=1
2023-09-21 11:06.45 [info     ] Transactions not pending       name=XXX records=48
2023-09-21 11:06.58 [info     ] Loaded table                   recods=662 year=2023
--------------------------------------------------------------------------------
My Bank - Checking
--------------------------------------------------------------------------------
2023-09-21 11:06.58 [info     ] Queried transactions           n=17
2023-09-21 11:06.58 [info     ] Pending transactions           n=0
2023-09-21 11:06.58 [info     ] New transactions to be added   n=2
2023-09-21 11:06.58 [info     ] Transaction                    transaction={'Account': 'BoA Checking', 'Amount': XXX, 'Category': '', 'Date': '2023-09-20', 'Description': 'XXXX', 'Subcategory': ''}
2023-09-21 11:06.58 [info     ] Transaction                    transaction={'Account': 'BoA Checking', 'Amount': XXX, 'Category': '', 'Date': '2023-09-20', 'Description': 'XXXX', 'Subcategory': ''}
2023-09-21 11:06.58 [info     ] Existing transactions          n=15
2023-09-21 11:06.58 [info     ] Error transactions             n=0

================================================================================
All accounts summary from 2023-08-22 to 2023-09-21:
================================================================================
Using Table for year: 2023
Records in table: 662
Transactions queried: 49
Pending transactions: 1
Transactions analysed (pending False): 48
New records to be inserted: 5
Existing transactions: 43
Errors transactions (not valid dates for table): 0

Insert transactions to the Table? [Y/n]
```

Generate a set of simple rules that will be used to categorize transactions:

```terminal
task rules
```

Generate a dataset with all transactions in CSV and SQLite:

```terminal
task dataset
```

## Setup

### Plaid Setup

- Create a [Plaid account](https://dashboard.plaid.com/)
- You would need development access to handle multiple accounts
- Get your `client_id` and `secret` from the [Plaid dashboard](https://dashboard.plaid.com/developers/keys)

#### Logging in to Banks

Use the [plaid/quickstart](https://github.com/plaid/quickstart.git)
to login to your institution.

Once you've linked the bank save the `ACCESS_TOKEN`. See [settings](#settings) below.

### NocoDB

Generate a Token using the UI and add it to the [settings](#settings).

Create one project `dinero` and one table per year inside that project.
Transactions will be added to the table for the current year.

## Settings

The project reads it's config from `~/.config/dinero/config.toml`.
You can see a sample in [config.sample.toml](config.sample.toml).

For Plaid you need to set the token:

```toml
[plaid.tokens]
BANK_1 = "access-development-XXXXXXXXXXXXXXXX"
```

And a mapping to make the Account ID human readable:

```toml
[plaid.account_id_to_name]
THIS_IS_A_LONG_ID_1_XXXXXXXXXXXXXXXXXXXXXX = "Bank 1 Checking"
```

## Contributions

While I am happy to accept any contributions this is 100% tailored to how I use
it so I might reject stuff I won't use.

If you want to do other things such as connecting to Airtable you should
fork this repo and make your own changes.

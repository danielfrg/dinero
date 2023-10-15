import logging

import pendulum
import structlog

from dinero import Application, nocodb, plaid
from dinero.utils import base as baseutils

structlog.configure(
    wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
)
log = structlog.get_logger()

##

DATE = pendulum.now()
# Explicitly set date
# DATE = pendulum.datetime(2022, 12, 31)
DAYS = 30
ADD_PENDING = False


def transactions():
    app = Application()

    # Get new transactions from plaid
    all_transactions = plaid.get_all_transactions(app, date=DATE, days=DAYS)

    # Load current year table
    table = nocodb.get_table(app, date=DATE)

    # Print individual accounts
    groups = all_transactions.group_by_account()
    for account_name, account_transactions in groups.items():
        print("-" * 80)
        print(account_name)
        print("-" * 80)

        new, existing, errored = table.prepare(account_transactions.not_pending)

        n_transactions = len(account_transactions)
        n_pending = len(account_transactions.pending)
        n_new, n_existing, n_error = len(new), len(existing), len(errored)

        log.info("Queried transactions", n=n_transactions)
        log.info("Pending transactions", n=n_pending)

        for transaction in account_transactions.pending:
            print(transaction)
        log.info("New transactions to be added", n=n_new)
        for transaction in new:
            log.info("Transaction", transaction=transaction)
        log.info("Existing transactions", n=n_existing)
        log.info("Error transactions", n=n_error)
        for transaction in errored:
            print(transaction)

        assert n_transactions == n_pending + n_new + n_existing + n_error
        print()

    # Print account summary
    new_records = all_transactions if ADD_PENDING else all_transactions.not_pending

    new, existing, errored = table.prepare(new_records)

    len_table = len(table)
    n_transactions = len(all_transactions)
    n_records = len(new_records)
    n_pending = len(all_transactions.pending)
    n_new = len(new)
    n_existing = len(existing)
    n_error = len(errored)

    start_date, end_date = baseutils.get_dates_from_delta(date=DATE, days=DAYS)

    print("=" * 80)
    print("All accounts summary from {} to {}:".format(start_date, end_date))
    print("=" * 80)
    print("Using Table for year: {}".format(DATE.year))
    print("Records in table: {}".format(len_table))
    print("Transactions queried: {}".format(n_transactions))
    print("Pending transactions: {}".format(n_pending))
    print("Transactions analysed (pending {}): {}".format(ADD_PENDING, n_records))
    print("New records to be inserted: {}".format(n_new))
    print("Existing transactions: {}".format(n_existing))
    print("Errors transactions (not valid dates for table): {}".format(n_error))
    print()

    assert n_records == n_new + n_existing + n_error

    if baseutils.noninteractive() or baseutils.query_yes_no(
        "Insert transactions to the Table?"
    ):
        print("Inserting %i new records" % len(table.new))
        table.commit()
        print("Done")


if __name__ == "__main__":
    import fire

    fire.Fire(transactions)

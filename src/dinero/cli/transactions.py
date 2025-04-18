import pendulum
from loguru import logger

from dinero import Application, db, plaid
from dinero.cli import utils
from dinero.utils import base as baseutils


DATE = pendulum.now()
# Explicitly set date
# DATE = pendulum.datetime(2022, 12, 31)
DAYS = 90
ADD_PENDING = False


def transactions():
    app = Application()

    # Get new transactions from plaid
    all_transactions = plaid.get_all_transactions(app, date=DATE, days=DAYS)

    table = db.Table(app)

    # Print individual accounts
    groups = all_transactions.group_by_account()
    for account_name, account_transactions in groups.items():
        print("-" * 80)
        print(account_name)
        print("-" * 80)

        new, existing = table.prepare(account_transactions.not_pending)

        n_transactions = len(account_transactions)
        n_pending = len(account_transactions.pending)
        n_new, n_existing = len(new), len(existing)

        logger.bind(n=n_transactions).info("Queried transactions")
        logger.bind(n=n_pending).info("Pending transactions")

        for transaction in account_transactions.pending:
            logger.info("Transaction", transaction=transaction)
        logger.bind(n=n_new).info("New transactions to be added")

        for transaction in new:
            logger.info("Transaction", transaction=transaction)
        logger.bind(n=n_existing).info("Existing transactions")

        # for transaction in existing:
        #     logger.info("Transaction", transaction=transaction)

        assert n_transactions == n_pending + n_new + n_existing
        print()

    # Print account summary
    new_records = all_transactions if ADD_PENDING else all_transactions.not_pending

    new, existing = table.prepare(new_records)

    len_table = len(table)
    n_transactions = len(all_transactions)
    n_records = len(new_records)
    n_pending = len(all_transactions.pending)
    n_new = len(new)
    n_existing = len(existing)

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
    print()

    assert n_records == n_new + n_existing

    if utils.noninteractive() or utils.query_yes_no(
        "Insert transactions to the Table?"
    ):
        print("Inserting %i new records" % len(table.new))
        table.commit()
        print("Done")


if __name__ == "__main__":
    import fire

    fire.Fire(transactions)

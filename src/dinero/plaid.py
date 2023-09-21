import pendulum
import structlog
from pendulum import Date
from plaid import Client
from plaid import errors as plaid_errors

from dinero.application import Application
from dinero.utils import base as baseutils

log = structlog.get_logger()

CLIENT = None


def get_client(app: Application):
    """Get a Plaid client"""
    global CLIENT
    if not CLIENT:
        plaid_client_id = app.config.plaid.client_id
        plaid_secret = app.config.plaid.secret
        # plaid_public_key = app.config.plaid.public_key
        plaid_env = app.config.plaid.env

        CLIENT = Client(
            client_id=plaid_client_id,
            secret=plaid_secret,
            # public_key=plaid_public_key,
            environment=plaid_env,
            suppress_warnings=True,
        )

    return CLIENT


def get_all_transactions(app: Application, date: str | Date, days=30):
    """Get transactions for all institutions"""
    all_transactions = TransactionList()
    for account_name, access_token in app.config.plaid.tokens.items():
        all_transactions.extend(
            get_transactions(app, account_name, date=date, days=days)
        )
    return all_transactions


def get_transactions(app: Application, name, date: str | Date, days=30):
    """Get transactions for one account based on the access token

    Parameters
    ----------
        name: Name of the institution on the config
        date: last day to end the transactions range
        days: number of days back from `date` to get transactions for
    """
    access_token = app.config.plaid.tokens[name]
    start_date, end_date = baseutils.get_dates_from_delta(date=date, days=days)

    # Get transactions in Plaid API JSON
    client = get_client(app)
    try:
        response = client.Transactions.get(
            access_token, start_date=start_date, end_date=end_date
        )
    except plaid_errors.ItemError as ex:
        if "the login details of this item have changed" in str(ex):
            raise Exception("Credentials expired for account: %s" % name)
        else:
            raise ex

    # This loop goes through the Plaid pagination
    transactions_api_json = response["transactions"]
    while len(transactions_api_json) < response["total_transactions"]:
        response = client.Transactions.get(
            access_token,
            start_date=start_date,
            end_date=end_date,
            offset=len(transactions_api_json),
        )
        transactions_api_json.extend(response["transactions"])
    log.info(
        "Transactions downloaded",
        name=name,
        records=len(transactions_api_json),
    )

    # From Plaid API JSON format to the classes in this file
    transactions = TransactionList()
    transactions.app = app

    for transaction_json in transactions_api_json:
        transactions.append_from_plaid(transaction_json)

    log.info(
        "Transactions pending",
        name=name,
        records=len(transactions.pending),
    )
    log.info(
        "Transactions not pending",
        name=name,
        records=len(transactions.not_pending),
    )
    return transactions


def account_id_to_name(app: Application, account_id):
    """Convert a Plaid account ID to a human readable name from the config"""
    plaid_id_to_name = app.config.plaid.account_id_to_name
    if account_id in plaid_id_to_name:
        return plaid_id_to_name[account_id]
    else:
        log.error(
            "ID not found in the accounts map, add it to the settings.toml "
            "on section: plaid.account_id_to_name",
            account_id=account_id,
        )
        return account_id


class TransactionList(list):
    app: Application

    @property
    def pending(self):
        """Filter by pending transactions
        Returns a new TransactionList
        """
        return TransactionList(filter(lambda x: x.pending, self))

    @property
    def not_pending(self):
        """Filter by not pending transactions
        Returns a new TransactionList
        """
        return TransactionList(filter(lambda x: not x.pending, self))

    def append(self, item, *args, **kwargs):
        if isinstance(item, Transaction):
            super(TransactionList, self).append(item, *args, **kwargs)
        else:
            raise ValueError(
                "TransactionsList class can only append Transaction objects"
            )

    def append_from_plaid(self, transaction_json):
        """Convert from plaid json to Transaction class and append"""
        new = Transaction.from_plaid(self.app, transaction_json)
        self.append(new)

    def group_by_account(self):
        """Returns a dictionary of {account_name: transaction_of_that_account}"""
        groups = {}
        for item in self:
            account_name = item.account_name
            if account_name in groups:
                groups[account_name].append(item)
            else:
                t_list = TransactionList()
                t_list.append(item)
                groups[account_name] = t_list
        return groups


class Transaction(object):
    def __init__(self, app):
        self.app: Application = app
        self.account_id = ""
        self.amount = 0
        self.category = []
        self.category_id = 0
        self.date: Date = pendulum.now()
        self.name = ""
        self.pending = False

    @classmethod
    def from_plaid(cls, app: Application, values):
        """Create a class from plaid API transaction JSON"""
        new = cls(app)
        new.account_id = values["account_id"]
        new.amount = values["amount"]
        new.category = values["category"]
        new.category_id = values["category_id"]
        new.date = pendulum.parse(values["date"])
        new.name = values["name"]
        new.pending = values["pending"]
        return new

    @property
    def account_name(self):
        return account_id_to_name(self.app, self.account_id)

    def __str__(self):
        return str(
            {
                "account_name": self.account_name,
                "description": self.name,
                "date": self.date.strftime("%Y-%m-%d"),
                "amount": self.amount,
            }
        )

    def __repr__(self):
        return str(self.__dict__)

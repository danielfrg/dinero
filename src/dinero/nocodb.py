import typing

import pendulum
import structlog
from nocodb.api import NocoDBProject
from nocodb.infra.requests_client import NocoDBRequestsClient
from nocodb.nocodb import APIToken
from pendulum import Date

from dinero.application import Application

log = structlog.get_logger()

client = None


def get_client(app):
    global client
    if client is None:
        client = NocoDBRequestsClient(
            APIToken(app.config.nocodb.token),
            app.config.nocodb.host,
        )
    return client


def get_table(app: Application, date: str | Date):
    """Get the Table for a date"""
    if isinstance(date, str):
        date = typing.cast(Date, pendulum.parse(date))
    date = date or pendulum.now()
    table = TransactionsTable(app, date.year)
    return table


class TransactionsTable:
    """Represents a yearly Transactions table

    Usage
    -----
        table.prepare(new_records)
        table.commit()
    """

    def __init__(self, app: Application, year, *args, **kwargs):
        self.app = app
        self.client = get_client(app)
        self.year = year

        # NocoDB specific
        self.table_name = str(year)
        self.project = NocoDBProject(app.config.nocodb.org, app.config.nocodb.project)

        # Internal state
        self.records = []
        self.new = []
        self.existing = []
        self.errored = []

        self.load()

    def __repr__(self):
        return "Transactions: {}".format(len(self.records))

    def __len__(self):
        return len(self.records)

    def load(self):
        """Load the records from the Table"""
        temp = []
        for i in range(2):
            # TODO: Be smarter about pagination
            offset = i * 1000
            rows = self.client.table_row_list(
                self.project, self.table_name, params={"limit": 1000, "offset": offset}
            )

            temp.extend(rows["list"])

        self.records = [Record(row) for row in temp]
        log.msg("Loaded table", year=self.year, recods=len(self.records))

    def prepare(self, transactions):
        """
        Checks:
            1. If the records to be inserted exist
            2. If its the correct table

        Parameters
        ----------
            transactions: TransactionList with new records to (possibly) be inserted

        Returns
        -------
            (existing, new, error): tuple of 3 lists, each list are Record
        """
        records = [Record.from_plaid_transaction(tr) for tr in transactions]

        self.new, self.existing, self.errored = [], [], []
        for record in records:
            if not self.exists(record):
                if record.date.year != self.year:
                    log.msg(
                        "Transaction date not valid for year",
                        year=self.year,
                        record=record,
                    )
                    self.errored.append(record.tojson())
                else:
                    self.new.append(record.tojson())
            else:
                # log.debug("Ignoring existing transaction", transaction=record)
                self.existing.append(record.tojson())

        return self.new, self.existing, self.errored

    def commit(self, *args, **kwargs):
        """Commit changes to NocoDB
        Run `self.prepare(records)` first
        """
        for record in self.new:
            self.client.table_row_create(self.project, self.table_name, record)

    def exists(self, record):
        """Check if the record exists"""
        for rec in self.records:
            if rec == record:
                return True
        return False


class Record(dict):
    @property
    def id(self):
        return self["ncRecordId"]

    @property
    def account(self):
        return self["Account"]

    @property
    def amount(self):
        return self["Amount"]

    @property
    def date(self):
        return self["Date"]

    @property
    def description(self):
        return self["Description"]

    @classmethod
    def from_plaid_transaction(cls, plaid_transaction):
        new = cls()
        category, subcategory = "", ""
        # category, subcategory = rules.categories_for_transaction(description)

        new.update(
            {
                "Account": plaid_transaction.account_name,
                "Amount": plaid_transaction.amount,
                "Category": category,
                "Date": plaid_transaction.date,
                "Description": plaid_transaction.name,
                "Subcategory": subcategory,
            }
        )
        return new

    def __eq__(self, other_record):
        # Checks uniqueness based on: Date, Account Name, Description, Amount
        date = "%s-%02d-%02d" % (
            other_record.date.year,
            other_record.date.month,
            other_record.date.day,
        )

        if (
            self.date == date
            and self.account == other_record.account
            and self.description == other_record.description
            and self.amount == other_record.amount
        ):
            return True
        return False

    def tojson(self):
        item = self.copy()

        # This is broken and I have no idea why
        # date = self.date.format("YYYY-MM-DD")
        date = "%s-%02d-%02d" % (
            self.date.year,
            self.date.month,
            self.date.day,
        )

        item["Date"] = date
        return item

import datetime

from numpy import select
from loguru import logger
from sqlalchemy import DateTime, Double, String, create_engine, select
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, sessionmaker

from dinero import rules
from dinero.application import Application


class Table:
    def __init__(self, app: Application):
        self.app = app
        self.session = get_session(app)

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
        stmt = select(Transaction)

        for transaction in self.session.scalars(stmt):
            self.records.append(transaction)

    def prepare(self, transactions):
        """
        Checks:
            1. If the records to be inserted exist
            2. If its the correct table

        Parameters
        ----------
            transactions: PlaidTransactions with new records to (possibly) be inserted

        Returns
        -------
            (existing, new, error): tuple of 3 lists, each list are Record
        """
        new_records = [Transaction.from_plaid(self.app, tr) for tr in transactions]

        self.new, self.existing = [], []

        for record in new_records:
            if not self.exists(record):
                self.new.append(record)
            else:
                self.existing.append(record)
            # break

        return self.new, self.existing

    def prepare_from_records(self, records: list["Transaction"]):
        """
        Prepare Transaction records for insertion (for CSV import).

        Unlike `prepare()` which converts from Plaid format, this takes
        already-created Transaction objects.

        Parameters
        ----------
            records: List of Transaction objects to (possibly) be inserted

        Returns
        -------
            (new, existing): tuple of 2 lists of Transaction objects
        """
        self.new, self.existing = [], []

        for record in records:
            if not self.exists(record):
                self.new.append(record)
            else:
                self.existing.append(record)

        return self.new, self.existing

    def commit(self, *args, **kwargs):
        """Commit changes to Postgres
        Run `self.prepare(records)` first
        """
        self.session.add_all(self.new)
        self.session.commit()

    def exists(self, new):
        """Check if the record exists"""
        for existing in self.records:
            if existing == new:
                return True
        return False

    def close(self):
        self.session.close()


def get_session(app: Application):
    engine = create_engine(app.config.database.connection_string)

    Session = sessionmaker(bind=engine)
    return Session()


class Base(DeclarativeBase):
    pass


class Transaction(Base):
    """The transactions table

    Usage
    -----
        table.prepare(new_records)
        table.commit()
    """

    __tablename__ = "transactions"

    id: Mapped[int] = mapped_column(primary_key=True, autoincrement=True)
    date: Mapped[datetime.date] = mapped_column(DateTime(timezone=True), nullable=True)
    description: Mapped[str] = mapped_column(String(1000), nullable=True)
    category: Mapped[str] = mapped_column(String(50), nullable=True)
    subcategory: Mapped[str] = mapped_column(String(50), nullable=True)
    amount: Mapped[float] = mapped_column(Double(), nullable=True)
    notes: Mapped[str] = mapped_column(String(1000), nullable=True)
    account: Mapped[str] = mapped_column(String(50), nullable=True)

    def __eq__(self, other_record):
        # Checks uniqueness based on: Date, Account Name, Description, Amount
        my_date = "%s-%02d-%02d" % (
            self.date.year,
            self.date.month,
            self.date.day,
        )
        date = "%s-%02d-%02d" % (
            other_record.date.year,
            other_record.date.month,
            other_record.date.day,
        )

        if (
            my_date == date
            and self.account == other_record.account
            and self.description == other_record.description
            and self.amount == other_record.amount
        ):
            return True
        return False

    @classmethod
    def from_plaid(cls, app: Application, plaid_transaction):
        new = cls()
        description = plaid_transaction.name
        category, subcategory = "", ""
        category, subcategory = rules.categories_for_transaction(app, description)

        new.date = plaid_transaction.date
        new.description = description
        new.category = category
        new.subcategory = subcategory
        new.amount = plaid_transaction.amount
        new.notes = "new"
        new.account = plaid_transaction.account_name

        return new

    def __repr__(self):
        return f"Transaction({self.id}): {self.date} {self.description} {self.amount}"

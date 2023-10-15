import logging

import pandas as pd
import structlog

from dinero import Application
from dinero.db import Transaction

pd.options.display.float_format = "{:,.2f}".format

structlog.configure(
    wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
)
log = structlog.get_logger()


app = Application()


def get_dataframe():
    """Iterate all the airtables and put it into a pandas.DataFrame"""
    con = app.config.database.connection_string
    return pd.read_sql(Transaction.__tablename__, con=con)


def select(data=None, year=None, month=None, before=None, after=None, account=None):
    selected = data if data is not None else get_dataframe()
    if account:
        selected = selected[selected.account == account]
    if year:
        selected = selected[selected.date.dt.year == year]
    if month:
        selected = selected[selected.date.dt.month == month]
    if after:
        selected = selected[selected.date >= after]
    if before:
        selected = selected[selected.date <= before]
    return selected

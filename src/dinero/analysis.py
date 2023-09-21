import logging
import os

import pandas as pd
import pendulum
import structlog

from dinero import Application
from dinero.nocodb import TransactionsTable

structlog.configure(
    wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
)
log = structlog.get_logger()

##

app = Application()


def get_data(no_cache=False, only_cache=False, years=None):
    if no_cache:
        df = get_dataframe(years=years, use_cache=False)
    if only_cache:
        df = load_data(app.config_file.cachedir / "data.csv")
    else:
        df = get_dataframe(years=years)
    return df


def get_dataframe(years=None, use_cache=True):
    """Iterate all the airtables and put it into a pandas.DataFrame"""

    today = pendulum.today()
    today_year = today.year
    start_year = 2012
    years = years or range(start_year, today_year + 1)

    data = pd.DataFrame()
    for year in years:
        table_name = str(year)

        if years and year not in years:
            continue

        # If its an older year, read from cache if available
        # If its not in the cache download it and cache it
        if year < today_year and use_cache:
            filepath = app.config_file.cachedir / f"{year}.csv"

            if os.path.exists(filepath):
                # Read cached file
                log.info("Reading cache", year=year, filepath=filepath)
                year_df = load_data(filepath)
            else:
                # Cached file not found -> download and cache
                year_df = get_table(table_name)

                if not os.path.exists(app.config_file.cachedir):
                    os.makedirs(app.config_file.cachedir, exist_ok=True)

                log.info("Saving cache", year=year, filepath=filepath)
                year_df.to_csv(filepath, index=False)
        else:
            # Download this current year or if ignoring cache
            year_df = get_table(table_name)

        data = pd.concat([data, year_df])

    data = data.sort_values("date", ascending=False)
    return data


def get_table(table_name):
    data = pd.DataFrame(
        columns=[
            "Date",
            "Description",
            "Category",
            "Subcategory",
            "Amount",
            "Notes",
            "Account",
        ]
    )

    table = TransactionsTable(app, table_name)
    for record in table.records:
        new_row = pd.DataFrame([record])
        data = pd.concat([data, new_row], ignore_index=True, sort=False)

    data.columns = data.columns.str.lower()

    data.date = pd.to_datetime(data.date)
    data.subcategory = data.subcategory.fillna("")

    data = data.sort_values("date", ascending=False)
    return data


def load_data(filepath):
    """Read a CSV saved from `make_dataframe`"""
    data = pd.read_csv(filepath)
    data.date = pd.to_datetime(data.date)
    return data


def group_desc_categories(data):
    """Group the data by
    Useful to see the most common transactions and generate rules
    """
    groups = data.groupby(["description", "category", "subcategory"]).count()
    groups = groups.amount.sort_values(ascending=False)
    return groups[groups >= 3]


def select(data=None, year=None, month=None, before=None, after=None, account=None):
    selected = data if data is not None else get_data()
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

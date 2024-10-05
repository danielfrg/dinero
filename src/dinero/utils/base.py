import calendar
import typing
from calendar import monthrange
from datetime import datetime

import pandas as pd
import pendulum
from pendulum import Date

today = datetime.now()
today = datetime(today.year, today.month, today.day, 23, 59, 59)
today = pd.to_datetime(today, utc=True)

def get_dates_from_month(date: str | Date):
    """
    Returns two strings with the first and end dates for a month
    """
    if isinstance(date, str):
        date = typing.cast(Date, pendulum.parse(date))
    date = date or pendulum.now()

    year, month = date.year, date.month
    first_weekday, last_day = monthrange(year, month)

    start_date = f"{year}-{month:02d}-01"
    end_date = f"{year}-{month:02d}-{last_day:02d}"
    return start_date, end_date


def get_dates_from_delta(date: str | Date, days=30):
    if isinstance(date, str):
        date = typing.cast(Date, pendulum.parse(date))
    end_date = date or pendulum.now()
    start_date = end_date.subtract(days=days)

    start_date = "{year}-{month:02d}-{day:02d}".format(
        year=start_date.year, month=start_date.month, day=start_date.day
    )
    end_date = "{year}-{month:02d}-{day:02d}".format(
        year=end_date.year, month=end_date.month, day=end_date.day
    )
    return start_date, end_date


def this_or_last_month(target_day=None):
    if target_day is None:
        target_day = today.day

    if target_day == -1:
        last_day_of_today_month = calendar.monthrange(today.year, today.month)[1]
        if today.day < last_day_of_today_month:
            if today.month == 1:
                target_month = 12
            else:
                target_month = today.month - 1
        else:
            target_month = today.month
        target_day = calendar.monthrange(today.year, target_month)[1]
    else:
        if target_day <= today.day:
            target_month = today.month
        else:
            if today.month == 1:
                target_month = 12
            else:
                target_month = today.month - 1

    if target_day >= today.day and target_month == 12:
        target_year = today.year - 1
    else:
        target_year = today.year

    dt = datetime(target_year, target_month, target_day, 23, 59, 59)
    return pd.to_datetime(dt, utc=True)

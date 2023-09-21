import os
import sys
import typing
from calendar import monthrange

import pendulum
from pendulum import Date


def noninteractive():
    dinero_yes = os.environ.get("DINERO_YES", "0")
    if dinero_yes != "0":
        return True
    return False


def query_yes_no(question, default="yes"):
    """Ask a yes/no question via raw_input() and return their answer.

    "question" is a string that is presented to the user.
    "default" is the presumed answer if the user just hits <Enter>.
            It must be "yes" (the default), "no" or None (meaning
            an answer is required of the user).

    The "answer" return value is True for "yes" or False for "no".
    """
    valid = {"yes": True, "y": True, "ye": True, "no": False, "n": False}
    if default is None:
        prompt = " [y/n] "
    elif default == "yes":
        prompt = " [Y/n] "
    elif default == "no":
        prompt = " [y/N] "
    else:
        raise ValueError("invalid default answer: '%s'" % default)

    while True:
        sys.stdout.write(question + prompt)
        choice = input().lower()
        if default is not None and choice == "":
            return valid[default]
        elif choice in valid:
            return valid[choice]
        else:
            sys.stdout.write("Please respond with 'yes' or 'no' " "(or 'y' or 'n').\n")


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

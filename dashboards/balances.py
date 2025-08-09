# A dashboard that shows the balance for all the accounts from accounts.yml

import streamlit as st
import pandas as pd
import yaml
from datetime import datetime, time

from dinero import analysis
from dinero.utils.base import today, this_or_last_month


def to_utc(dt):
    dt = pd.to_datetime(dt)
    if dt.tzinfo is None:
        dt = dt.tz_localize("UTC")
    return dt


# Load configuration from YAML file
with open("accounts.yml", "r") as f:
    config = yaml.safe_load(f)

df = analysis.get_dataframe()

accounts = {}

# Create a sidebar date input for each account based on configuration
for account in config["accounts"]:
    name = account["name"]
    default_config = account["default"]

    if default_config["method"] == "today":
        default_date = today
    elif default_config["method"] == "this_or_last_month":
        offset = default_config.get("offset", 0)
        default_date = this_or_last_month(offset)
    else:
        default_date = today  # fallback

    user_date = st.sidebar.date_input(name, default_date)

    # Combine the selected date with the end-of-day time to make the filter inclusive
    end_of_day_datetime = datetime.combine(user_date, time(23, 59, 59))
    accounts[name] = to_utc(end_of_day_datetime)

# Process each account's data
values = []
for account_name, account_date in accounts.items():
    # The 'before' parameter will now correctly include the entire selected day
    data = analysis.select(df, account=account_name, before=account_date)
    balance = "{:,.2f}".format(data.amount.sum())
    values.append(
        {"Account": account_name, "Date": account_date.date(), "Balance": balance}
    )

accounts_df = pd.DataFrame(values).set_index("Account")

st.write("# Balances per Date")
st.dataframe(accounts_df, use_container_width=True)

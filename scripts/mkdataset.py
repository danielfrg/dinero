import os

from sqlalchemy import create_engine

from dinero import analysis

if not os.path.exists("data"):
    os.makedirs("data", exist_ok=True)

file_path = "data/all.csv"

df = analysis.get_dataframe(use_cache=False)
df.to_csv(file_path, index=False)


engine = create_engine("sqlite:///data/transactions.db")

with engine.connect() as conn:
    df.to_sql("transactions", conn, if_exists="replace", index=False)

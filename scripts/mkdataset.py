from sqlalchemy import create_engine

from dinero import analysis

file_path = "cache/all.csv"

df = analysis.get_dataframe()
df.to_csv(file_path, index=False)


engine = create_engine("sqlite:///cache/transactions.db")

with engine.connect() as conn:
    df.to_sql("transactions", conn, if_exists="replace")

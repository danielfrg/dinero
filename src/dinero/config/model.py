from typing import Dict

from pydantic import BaseModel, BaseSettings


class Plaid(BaseModel):
    client_id: str
    secret: str
    env: str
    products: str
    tokens: Dict[str, str]
    account_id_to_name: Dict[str, str]


class Database(BaseModel):
    connection_string: str


class RootConfig(BaseSettings):
    plaid: Plaid
    database: Database

    class Config:
        env_prefix = "dinero_"

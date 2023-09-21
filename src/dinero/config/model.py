from typing import Dict

from pydantic import BaseModel, BaseSettings

from dinero.utils.fs import repo_root


class Plaid(BaseModel):
    client_id: str
    secret: str
    env: str
    products: str
    tokens: Dict[str, str]
    account_id_to_name: Dict[str, str]


class NocoDB(BaseModel):
    host: str
    token: str
    project: str
    org: str = "noco"


class RootConfig(BaseSettings):
    plaid: Plaid
    nocodb: NocoDB

    class Config:
        env_prefix = "dinero_"

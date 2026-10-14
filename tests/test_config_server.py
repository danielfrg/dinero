from dinero.config.model import RootConfig


def test_python_config_accepts_go_server_section():
    config = RootConfig.parse_obj(
        {
            "plaid": {"client_id": "", "secret": "", "env": "development", "products": "transactions", "tokens": {}, "account_id_to_name": {}},
            "database": {"connection_string": "sqlite:///synthetic.db"},
            "server": {"host": "127.0.0.1", "port": 8080, "allowed_hosts": ["localhost"], "allowed_origins": []},
        }
    )
    assert config.server["port"] == 8080

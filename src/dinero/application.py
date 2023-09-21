from typing import Union

from dinero.config.config_file import ConfigFile
from dinero.config.model import RootConfig
from dinero.utils.fs import Path


class Application:
    config_file_path: Union[Path, None]
    config_file: ConfigFile

    def __init__(
        self,
        config_file: Union[str, None] = None,  # Path to config file
        load_config=True,
    ):
        if config_file is None:
            self.config_file_path = ConfigFile.get_default_location()
            self.config_file = ConfigFile()
        else:
            self.config_file_path = Path(config_file)
            path_ = None if config_file is None else Path(config_file)
            self.config_file = ConfigFile(path=path_)

        if load_config:
            self.load_config()

    def load_config(self):
        self.config_file.load()

    @property
    def config(self) -> RootConfig:
        return self.config_file.model

    @property
    def config_dir(self):
        return self.config_file.configdir()

    @property
    def cache_dir(self):
        return self.config_file.configdir() / "cache"


Dinero = Application

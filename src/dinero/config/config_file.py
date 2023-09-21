import sys
from typing import Union, cast

from dinero.config.model import RootConfig
from dinero.utils.fs import Path, load_toml_data

from platformdirs import PlatformDirs as ResultPD

if sys.platform == "darwin":
    from platformdirs.unix import Unix as ResultPD


class ConfigFile:
    model: RootConfig

    def __init__(self, path: Union[Path, None] = None):
        self._path: Union[Path, None] = path
        self.model = cast(RootConfig, None)

    @property
    def path(self):
        if self._path is None:
            self._path = self.get_default_location()
        return self._path

    @path.setter
    def path(self, value):
        self._path = value

    def save(self, content=None):
        import tomli_w

        if not content:
            content = tomli_w.dumps(self.model.dict(exclude_none=True))

        self.path.ensure_parent_dir_exists()
        self.path.write_atomic(content, "w", encoding="utf-8")

    def load(self):
        self.model = RootConfig.parse_obj(load_toml_data(self.read()))

    def read(self) -> str:
        return self.path.read_text("utf-8")

    # def restore(self):
    #     import tomli_w

    #     config = RootConfig()

    #     content = tomli_w.dumps(config.dict(exclude_none=True))
    #     self.save(content)

    #     self.model = config

    def update(self):
        self.save()

    @property
    def cachedir(self):
        platform_dir = ResultPD("dinero", roaming=True)
        return Path(platform_dir.user_config_dir) / "cache"

    @classmethod
    def get_default_location(cls) -> Path:
        platform_dir = ResultPD("dinero", roaming=True)
        return Path(platform_dir.user_config_dir) / "config.toml"

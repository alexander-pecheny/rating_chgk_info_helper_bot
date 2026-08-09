import json
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Config:
    data_dir: Path
    token: str
    admins: frozenset[int]
    announce_channel_id: int
    debug: bool

    @property
    def bot_db(self) -> Path:
        return self.data_dir / "bot.db"

    @property
    def logs_db(self) -> Path:
        return self.data_dir / "logs.db"


def load(data_dir: Path, token_path: Path | None, debug: bool) -> Config:
    raw = json.loads((data_dir / "config.json").read_text())
    token_path = token_path or data_dir / "token"
    return Config(
        data_dir=data_dir,
        token=token_path.read_text().strip(),
        admins=frozenset(raw["admins"]),
        announce_channel_id=raw["announce_channel_id"],
        debug=debug,
    )

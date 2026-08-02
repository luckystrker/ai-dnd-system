from collections.abc import Collection
from enum import Enum


class Visibility(str, Enum):
    PUBLIC = "public"
    PARTY = "party"
    PRIVATE = "private"
    DM_ONLY = "dm_only"
    REGIONAL = "regional"


def is_visible_to(
    visibility: Visibility | str,
    *,
    observer_id: str | None = None,
    visibility_target: str | None = None,
    is_dm: bool = False,
    audience: str = "party",
    observer_region: str | None = None,
    tags: Collection[str] = (),
) -> bool:
    """Apply the event visibility policy without consulting a database."""
    try:
        visibility = Visibility(visibility)
    except ValueError:
        return False

    if is_dm:
        return True
    if visibility is Visibility.DM_ONLY:
        return False
    if visibility is Visibility.PUBLIC:
        return True
    if visibility is Visibility.PARTY:
        return audience in {"party", "player"}
    if visibility is Visibility.PRIVATE:
        return observer_id is not None and observer_id == visibility_target
    if visibility is Visibility.REGIONAL:
        if observer_region is None:
            return False
        return (
            observer_region in tags
            or f"region:{observer_region}" in tags
            or f"location:{observer_region}" in tags
        )
    return False

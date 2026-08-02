from fastapi import APIRouter, HTTPException, status

from app.engine.registry import system_registry
from app.services.system_service import system_summary

router = APIRouter(prefix="/systems", tags=["systems"])


@router.get("")
async def list_systems() -> dict[str, list[str]]:
    return {"systems": system_registry.available()}


@router.get("/{system_name}")
async def get_system(system_name: str) -> dict:
    try:
        return system_summary(system_name)
    except ValueError as error:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail=str(error)
        ) from error

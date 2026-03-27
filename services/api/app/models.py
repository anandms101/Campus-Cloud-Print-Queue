from pydantic import BaseModel
from typing import Optional


class ReleaseRequest(BaseModel):
    printerName: str


class JobResponse(BaseModel):
    jobId: str
    userId: str
    fileName: str
    status: str
    printerName: Optional[str] = None
    createdAt: str
    updatedAt: str


class HealthResponse(BaseModel):
    status: str
    timestamp: str

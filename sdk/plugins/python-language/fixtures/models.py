"""
Domain models — demonstrates dataclasses, type aliases, enums, and NewType.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Optional


class UserRole(Enum):
    GUEST  = "guest"
    USER   = "user"
    ADMIN  = "admin"


class AccountStatus(Enum):
    ACTIVE    = "active"
    SUSPENDED = "suspended"
    DELETED   = "deleted"


@dataclass
class User:
    id:         str
    email:      str
    name:       str
    role:       UserRole       = UserRole.USER
    status:     AccountStatus  = AccountStatus.ACTIVE
    created_at: datetime       = field(default_factory=datetime.utcnow)
    updated_at: datetime       = field(default_factory=datetime.utcnow)

    def is_active(self) -> bool:
        return self.status == AccountStatus.ACTIVE

    def is_admin(self) -> bool:
        return self.role == UserRole.ADMIN

    def display_name(self) -> str:
        return self.name or self.email.split("@")[0]


@dataclass
class CreateUserInput:
    email: str
    name:  str
    role:  UserRole = UserRole.USER


@dataclass
class UpdateUserInput:
    name:   Optional[str]        = None
    role:   Optional[UserRole]   = None
    status: Optional[AccountStatus] = None


@dataclass
class PaginatedResult:
    items:   list
    total:   int
    page:    int
    per_page: int

    @property
    def total_pages(self) -> int:
        if self.per_page == 0:
            return 0
        return (self.total + self.per_page - 1) // self.per_page

    @property
    def has_next(self) -> bool:
        return self.page < self.total_pages

    @property
    def has_prev(self) -> bool:
        return self.page > 1


# Type aliases
UserID    = str
ProjectID = str

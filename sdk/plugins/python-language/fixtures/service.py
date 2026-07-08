"""
User service — demonstrates async methods, type hints, decorators, and ABC usage.
"""
from __future__ import annotations

import hashlib
import logging
from abc import ABC, abstractmethod
from typing import Optional

from .models import User, CreateUserInput
from .db import database

logger = logging.getLogger(__name__)

MAX_EMAIL_LENGTH = 254


class UserRepository(ABC):
    """Abstract base class for user data access."""

    @abstractmethod
    async def find_by_id(self, user_id: str) -> Optional[User]:
        ...

    @abstractmethod
    async def find_by_email(self, email: str) -> Optional[User]:
        ...

    @abstractmethod
    async def create(self, input: CreateUserInput) -> User:
        ...

    @abstractmethod
    async def update(self, user_id: str, patch: dict) -> User:
        ...

    @abstractmethod
    async def delete(self, user_id: str) -> None:
        ...


class UserService(UserRepository):
    """Concrete user service backed by a database repository."""

    def __init__(self, repo: UserRepository) -> None:
        self._repo = repo

    async def find_by_id(self, user_id: str) -> Optional[User]:
        return await self._repo.find_by_id(user_id)

    async def find_by_email(self, email: str) -> Optional[User]:
        if len(email) > MAX_EMAIL_LENGTH:
            raise ValueError(f"Email too long: {len(email)} > {MAX_EMAIL_LENGTH}")
        return await self._repo.find_by_email(email)

    async def create(self, input: CreateUserInput) -> User:
        _validate_create_input(input)
        return await self._repo.create(input)

    async def update(self, user_id: str, patch: dict) -> User:
        existing = await self.find_by_id(user_id)
        if existing is None:
            raise UserNotFoundError(user_id)
        return await self._repo.update(user_id, patch)

    async def delete(self, user_id: str) -> None:
        return await self._repo.delete(user_id)


class UserNotFoundError(Exception):
    """Raised when a user cannot be located."""

    def __init__(self, user_id: str) -> None:
        super().__init__(f"User not found: {user_id}")
        self.user_id = user_id


def _validate_create_input(input: CreateUserInput) -> None:
    if not input.email or "@" not in input.email:
        raise ValueError("Invalid email address")
    if not input.name or not input.name.strip():
        raise ValueError("Name must not be blank")


def hash_password(password: str, salt: Optional[str] = None) -> tuple[str, str]:
    if salt is None:
        import secrets
        salt = secrets.token_hex(16)
    digest = hashlib.sha256(f"{salt}{password}".encode()).hexdigest()
    return digest, salt


def create_user_service(repo: Optional[UserRepository] = None) -> UserService:
    from .db import DatabaseUserRepository
    return UserService(repo or DatabaseUserRepository(database))

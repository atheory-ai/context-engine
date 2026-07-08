import React, { useState, useCallback } from "react"
import type { User } from "./types"

interface UserCardProps {
  user: User
  onEdit?: (user: User) => void
  onDelete?: (id: string) => void
}

export function UserCard({ user, onEdit, onDelete }: UserCardProps) {
  const [expanded, setExpanded] = useState(false)

  const handleDelete = useCallback(() => {
    onDelete?.(user.id)
  }, [user.id, onDelete])

  return (
    <div className="user-card">
      <h3>{user.name}</h3>
      <p>{user.email}</p>
      {expanded && <pre>{JSON.stringify(user, null, 2)}</pre>}
      <button onClick={() => setExpanded(e => !e)}>
        {expanded ? "collapse" : "expand"}
      </button>
      {onEdit && <button onClick={() => onEdit(user)}>Edit</button>}
      {onDelete && <button onClick={handleDelete}>Delete</button>}
    </div>
  )
}

interface UserListProps {
  users: User[]
  onSelect?: (user: User) => void
}

export function UserList({ users, onSelect }: UserListProps) {
  return (
    <ul>
      {users.map(u => (
        <li key={u.id} onClick={() => onSelect?.(u)}>
          <UserCard user={u} />
        </li>
      ))}
    </ul>
  )
}

export default function UsersPage() {
  const [users] = useState<User[]>([])
  return <UserList users={users} />
}

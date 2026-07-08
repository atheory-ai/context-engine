import { z } from "zod"
import type { User, CreateUserInput } from "./types"
import { db } from "../db/client"

export interface UserRepository {
  findById(id: string): Promise<User | null>
  findByEmail(email: string): Promise<User | null>
  create(input: CreateUserInput): Promise<User>
  update(id: string, patch: Partial<User>): Promise<User>
  delete(id: string): Promise<void>
}

const CreateUserSchema = z.object({
  email: z.string().email(),
  name:  z.string().min(1),
})

export class UserService implements UserRepository {
  constructor(private readonly repo: UserRepository) {}

  async findById(id: string): Promise<User | null> {
    return this.repo.findById(id)
  }

  async findByEmail(email: string): Promise<User | null> {
    return this.repo.findByEmail(email)
  }

  async create(input: CreateUserInput): Promise<User> {
    const validated = CreateUserSchema.parse(input)
    return this.repo.create(validated)
  }

  async update(id: string, patch: Partial<User>): Promise<User> {
    const existing = await this.findById(id)
    if (!existing) throw new Error(`User not found: ${id}`)
    return this.repo.update(id, patch)
  }

  async delete(id: string): Promise<void> {
    return this.repo.delete(id)
  }
}

export type ServiceFactory<T> = (deps: Record<string, unknown>) => T

export const createUserService: ServiceFactory<UserService> = (deps) => {
  return new UserService(deps["repo"] as UserRepository)
}

import { stat, readdir } from "node:fs/promises"
import { homedir } from "node:os"
import { dirname, join, resolve } from "node:path"

export type DirectoryEntry = {
  name: string
  path: string
}

export type DirectoryListing = {
  currentPath: string
  parentPath: string | null
  directories: DirectoryEntry[]
  isGitRepository: boolean
}

export type DirectoryPickerItem =
  | { kind: "select"; label: string; path: string }
  | { kind: "parent"; label: string; path: string }
  | { kind: "directory"; label: string; path: string }

export function initialDirectory(): string {
  return homedir()
}

export async function listDirectories(directory: string): Promise<DirectoryListing> {
  const currentPath = resolve(directory)
  const entries = await readdir(currentPath, { withFileTypes: true })
  const directories: DirectoryEntry[] = []

  for (const entry of entries) {
    if (entry.name === ".git") {
      continue
    }

    const entryPath = join(currentPath, entry.name)
    let isDirectory = entry.isDirectory()
    if (!isDirectory && entry.isSymbolicLink()) {
      try {
        isDirectory = (await stat(entryPath)).isDirectory()
      } catch {
        isDirectory = false
      }
    }

    if (isDirectory) {
      directories.push({ name: entry.name, path: entryPath })
    }
  }

  directories.sort((left, right) => {
    const leftHidden = left.name.startsWith(".")
    const rightHidden = right.name.startsWith(".")
    if (leftHidden !== rightHidden) {
      return leftHidden ? 1 : -1
    }
    return left.name.localeCompare(right.name, undefined, { sensitivity: "base" })
  })

  const parent = dirname(currentPath)
  return {
    currentPath,
    parentPath: parent === currentPath ? null : parent,
    directories,
    isGitRepository: await pathExists(join(currentPath, ".git")),
  }
}

export function buildDirectoryPickerItems(listing: DirectoryListing): DirectoryPickerItem[] {
  const items: DirectoryPickerItem[] = [
    {
      kind: "select",
      label: "Use this folder",
      path: listing.currentPath,
    },
  ]

  if (listing.parentPath) {
    items.push({ kind: "parent", label: "../", path: listing.parentPath })
  }

  for (const directory of listing.directories) {
    items.push({ kind: "directory", label: `${directory.name}/`, path: directory.path })
  }
  return items
}

export function visibleDirectoryItems<T>(items: T[], selectedIndex: number, limit = 12): Array<{
  item: T
  index: number
}> {
  if (items.length <= limit) {
    return items.map((item, index) => ({ item, index }))
  }

  const half = Math.floor(limit / 2)
  const start = Math.max(0, Math.min(selectedIndex - half, items.length - limit))
  return items.slice(start, start + limit).map((item, offset) => ({ item, index: start + offset }))
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await stat(path)
    return true
  } catch {
    return false
  }
}

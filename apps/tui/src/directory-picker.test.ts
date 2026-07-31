import { describe, expect, test } from "bun:test"
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { dirname, join } from "node:path"

import {
  buildDirectoryPickerItems,
  listDirectories,
  visibleDirectoryItems,
} from "./directory-picker"

describe("directory picker", () => {
  test("lists folders, marks Git repositories, and supports directory symlinks", async () => {
    const root = await mkdtemp(join(tmpdir(), "orkoda-directory-picker-"))
    try {
      await mkdir(join(root, "Beta"))
      await mkdir(join(root, "alpha"))
      await mkdir(join(root, ".hidden"))
      await mkdir(join(root, ".git"))
      await writeFile(join(root, "README.md"), "not a directory")
      await symlink(join(root, "alpha"), join(root, "linked"))

      const listing = await listDirectories(root)

      expect(listing.currentPath).toBe(root)
      expect(listing.parentPath).toBe(dirname(root))
      expect(listing.isGitRepository).toBe(true)
      expect(listing.directories.map((entry) => entry.name)).toEqual([
        "alpha",
        "Beta",
        "linked",
        ".hidden",
      ])
    } finally {
      await rm(root, { recursive: true, force: true })
    }
  })

  test("builds select and parent actions before child directories", () => {
    const items = buildDirectoryPickerItems({
      currentPath: "/home/user/projects",
      parentPath: "/home/user",
      isGitRepository: false,
      directories: [{ name: "app", path: "/home/user/projects/app" }],
    })

    expect(items.map((item) => item.kind)).toEqual(["select", "parent", "directory"])
    expect(items[0]?.path).toBe("/home/user/projects")
    expect(items[2]?.label).toBe("app/")
  })

  test("keeps the selected directory inside the visible window", () => {
    const items = Array.from({ length: 20 }, (_, index) => `folder-${index}`)
    const visible = visibleDirectoryItems(items, 15, 5)

    expect(visible).toHaveLength(5)
    expect(visible.some((entry) => entry.index === 15)).toBe(true)
    expect(visible.map((entry) => entry.index)).toEqual([13, 14, 15, 16, 17])
  })
})

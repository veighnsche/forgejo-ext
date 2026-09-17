# Meta repositories

This file is meant to provide information on dealing with the repositories that
are bundled in Forgejo's worktree for testing (e.g. integration tests).

It was authored in a very ad-hoc manner to cover a daunting knowledge gap.

## Repository modifications

### Using the `git` CLI

The repositories stored in this folder are in bare format. If you need to
modify the contents of a repository, you first need to clone the repository
locally:

```
git clone -l example.git example-new.git
```

You should then be able to modify the repository as you wish.
After you're done, remove `example.git` and re-convert `example-new.git` into a
bare repository:

```
git clone --bare example-new.git
mv example-new.git example.git
```

### Using a local Forgejo instance

Simply create a local Forgejo instance, use
`git clone -l example.git example-new.git`, modify `example-new.git` as you
wish, then push it to your local Forgejo instance.

## LFS

"LFS files" are backed by a local Git LFS storage under
`./tests/gitea-lfs-meta`; if you wish to include/modify the Git LFS files (and
not just the Git LFS pointers stored in the `.git` repository), a quick way of
doing this would be to create a local Forgejo instance,

- Clone the bare repository.

```
git clone -l example.git example-new.git
```

- Create a new repository to Forgejo, then push it.
- Modify the repository and push it to your local Forgejo instance.

### Missing / Unassociated preexisting LFS files

If you convert the Git repository from bare format to an ordinary format
locally (before pushing it onto Forgejo), the repository in Forgejo might be
missing some preexisting LFS objects. In that case,
`cp ./tests/git-lfs-meta/* data/lfs/`, then, in your repository settings, head
to `Settings > LFS > Pointer files > Auto-associate X OIDs`.

## Copying modified repository data back to the `tests/` folder

- After you modify the repository and push your changes, modify the test
  repositories folder:
  - `mv tests/gitea-repositories-meta/user2/lfs.git tests/gitea-repositories-meta/user2/lfs.git.bak`
  - `cp -r data/forgejo-repositories/lfs.git tests/gitea-repositories-meta/user2/lfs.git`
- If applicable, copy the local LFS store: `cp -r data/lfs/* tests/gitea-lfs-meta/`
- Ensure that "leftover" files are not included in the repository, e.g.
  `tests/gitea-repository-meta/user/lfs.git/logs` when copying the repository
  back from your local Forgejo instance.

## Modifying fixtures

Fixtures are a means of representing "Forgejo's database" in a more
human-readable format, without having to ship (and constantly update) `.db`
files (which don't look as nice in Git diffs).

Ensure that your modifications don't break the assumptions defined in
`models/fixtures/repository.yml`; you might need/want to modify said fixtures
as necessary. For example, if you modify a repository to include multiple
branches, you should modify `models/fixtures/branch.yml`.

If you added new LFS files (or modified existing ones, which should result in
the creation of new "LFS meta objects"), you might want to update
`models/fixtures/lfs_meta_object.yml` and any affected tests.

There are tests that depend on fixtures and meta repositories; if you modify
what the tests depend on, there is a possibility that they might stop working,
and that some (hopefully minor) modifications might be needed for them to work
again.

# Generator upgrades

Inspect the relevant root, server, and SDK `buf.yaml`, `buf.lock`, and
`buf.gen.yaml` files. Verify proposed versions and API behavior against live
upstream sources. A generator upgrade can legitimately change every output
it produces; explain that scope and validate consumers of changed APIs.

Update Buf lock data through the tool, not by hand. Confirm a lock diff has
an intentional dependency update behind it. Run `just gen`, group output by
language, and investigate unrelated changes. Do not select an arbitrary older
version merely to shrink the diff; keep generator and consumer versions coherent.

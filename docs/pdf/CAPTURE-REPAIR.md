# Native capture helper repair

The first Z3 setup capture stopped before building: `Snapshot renderer changed; review scaled allocation before capture`.
The helper expected `bitmapImageRepForCachingDisplay`; SnapshotRunner now explicitly allocates an NSBitmapImageRep.

This separate repair changes only the disposable-copy allocation match and its scaled pixel dimensions.
The tracked app renderer remains unchanged. A missing or ambiguous match still stops capture.
No bundle, helper service or live backend is launched. Validation belongs to the resumed setup capture.

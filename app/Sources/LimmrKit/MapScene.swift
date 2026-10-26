/// Stable seed shared by map placement and tinting.
public enum MapSceneMotion {
  public static func stableHash(_ value: String) -> UInt64 {
    value.utf8.reduce(UInt64(14_695_981_039_346_656_037)) { ($0 ^ UInt64($1)) &* 1_099_511_628_211 }
  }
}

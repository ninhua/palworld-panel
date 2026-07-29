#[cfg(test)]
use std::fs;
use std::path::{Path, PathBuf};

use serde::Serialize;
use thiserror::Error;

use crate::{remap_world, MappingSet, RemapError, RemapOptions, VerificationReport};

pub const SOURCE_HOST_UID: &str = "00000000-0000-0000-0000-000000000001";

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct HostMigrationPlan {
    pub steam_id: String,
    pub source_uid: String,
    pub target_uid: String,
    pub strategy: String,
    pub can_execute: bool,
    pub source_player_file: String,
    pub source_dps_exists: bool,
    pub target_player_exists: bool,
    pub target_dps_exists: bool,
    pub warnings: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct HostMigrationResult {
    pub plan: HostMigrationPlan,
    pub verification: VerificationReport,
}

#[derive(Debug, Error)]
pub enum HostMigrationError {
    #[error("SteamID64 must contain 15 to 20 decimal digits")]
    InvalidSteamId,
    #[error("world root must contain Level.sav and Players/")]
    InvalidWorld,
    #[error("co-op host player file is missing: {0}")]
    SourcePlayerMissing(PathBuf),
    #[error("target UID already exists; automatic target cleanup is not enabled: {0}")]
    TargetExists(String),
    #[error("host UID is already the derived target UID")]
    AlreadyMigrated,
    #[error("could not create UID mapping: {0}")]
    Mapping(String),
    #[error(transparent)]
    Remap(#[from] RemapError),
    #[error("I/O failed for {path}: {source}")]
    Io {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
}

pub fn steam_id_to_player_uid(steam_id: &str) -> Result<String, HostMigrationError> {
    let steam_id = steam_id.trim();
    if !(15..=20).contains(&steam_id.len()) || !steam_id.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(HostMigrationError::InvalidSteamId);
    }
    let utf16le = steam_id
        .encode_utf16()
        .flat_map(u16::to_le_bytes)
        .collect::<Vec<_>>();
    let hashed = city_hash64(&utf16le);
    let value = (hashed as u32).wrapping_add(((hashed >> 32) as u32).wrapping_mul(23));
    Ok(format!("{value:08x}-0000-0000-0000-000000000000"))
}

pub fn analyze_host_migration(
    input_dir: impl AsRef<Path>,
    steam_id: &str,
) -> Result<HostMigrationPlan, HostMigrationError> {
    let input = input_dir.as_ref();
    let players = input.join("Players");
    if !input.join("Level.sav").is_file() || !players.is_dir() {
        return Err(HostMigrationError::InvalidWorld);
    }
    let target_uid = steam_id_to_player_uid(steam_id)?;
    let source_name = player_file_name(SOURCE_HOST_UID);
    let source_file = players.join(&source_name);
    if !source_file.is_file() {
        return Err(HostMigrationError::SourcePlayerMissing(source_file));
    }
    let source_dps_exists = players.join(dps_file_name(SOURCE_HOST_UID)).is_file();
    let target_player_exists = players.join(player_file_name(&target_uid)).exists();
    let target_dps_exists = players.join(dps_file_name(&target_uid)).exists();
    let already_migrated = target_uid == SOURCE_HOST_UID;
    let blocked = target_player_exists || target_dps_exists;
    let mut warnings = Vec::new();
    let strategy = if already_migrated {
        warnings.push("主机 UID 已与目标 UID 相同，无需迁移。".to_owned());
        "already_migrated"
    } else if blocked {
        warnings.push(
            "目标 UID 已存在。当前安全接入不会自动删除临时角色、帕鲁、公会或基地；请使用尚未加入过专服的存档，或先在离线副本中完成目标角色清理。".to_owned(),
        );
        "target_exists"
    } else {
        "direct"
    };
    Ok(HostMigrationPlan {
        steam_id: steam_id.trim().to_owned(),
        source_uid: SOURCE_HOST_UID.to_owned(),
        target_uid,
        strategy: strategy.to_owned(),
        can_execute: !already_migrated && !blocked,
        source_player_file: format!("Players/{source_name}"),
        source_dps_exists,
        target_player_exists,
        target_dps_exists,
        warnings,
    })
}

pub fn execute_host_migration(
    input_dir: impl AsRef<Path>,
    output_dir: impl AsRef<Path>,
    steam_id: &str,
) -> Result<HostMigrationResult, HostMigrationError> {
    let input = input_dir.as_ref();
    let plan = analyze_host_migration(input, steam_id)?;
    if plan.strategy == "already_migrated" {
        return Err(HostMigrationError::AlreadyMigrated);
    }
    if !plan.can_execute {
        return Err(HostMigrationError::TargetExists(plan.target_uid.clone()));
    }
    let mapping_json = serde_json::to_vec(&serde_json::json!({
        "source_uid": plan.source_uid,
        "target_uid": plan.target_uid,
    }))
    .map_err(|error| HostMigrationError::Mapping(error.to_string()))?;
    let mapping = MappingSet::from_json(&mapping_json)
        .map_err(|error| HostMigrationError::Mapping(error.to_string()))?;
    let verification = remap_world(input, output_dir, &mapping, &RemapOptions::default())?;
    Ok(HostMigrationResult { plan, verification })
}

fn player_file_name(uid: &str) -> String {
    format!("{}.sav", uid.replace('-', "").to_uppercase())
}

fn dps_file_name(uid: &str) -> String {
    format!("{}_dps.sav", uid.replace('-', "").to_uppercase())
}

const K0: u64 = 0xc3a5c85c97cb3127;
const K1: u64 = 0xb492b66fbe98f273;
const K2: u64 = 0x9ae16a3b2f90404f;
const K_MUL: u64 = 0x9ddfea08eb382d69;

fn fetch64(input: &[u8], offset: usize) -> u64 {
    u64::from_le_bytes(input[offset..offset + 8].try_into().expect("eight bytes"))
}

fn fetch32(input: &[u8], offset: usize) -> u32 {
    u32::from_le_bytes(input[offset..offset + 4].try_into().expect("four bytes"))
}

fn rotate(value: u64, shift: u32) -> u64 {
    value.rotate_right(shift)
}

fn shift_mix(value: u64) -> u64 {
    value ^ (value >> 47)
}

fn hash_len_16_mul(u: u64, v: u64, mul: u64) -> u64 {
    let mut a = (u ^ v).wrapping_mul(mul);
    a ^= a >> 47;
    let mut b = (v ^ a).wrapping_mul(mul);
    b ^= b >> 47;
    b.wrapping_mul(mul)
}

fn hash_len_16(u: u64, v: u64) -> u64 {
    hash_len_16_mul(u, v, K_MUL)
}

fn hash_len_0_to_16(input: &[u8]) -> u64 {
    let len = input.len();
    if len >= 8 {
        let mul = K2.wrapping_add((len as u64).wrapping_mul(2));
        let a = fetch64(input, 0).wrapping_add(K2);
        let b = fetch64(input, len - 8);
        let c = rotate(b, 37).wrapping_mul(mul).wrapping_add(a);
        let d = rotate(a, 25).wrapping_add(b).wrapping_mul(mul);
        return hash_len_16_mul(c, d, mul);
    }
    if len >= 4 {
        let mul = K2.wrapping_add((len as u64).wrapping_mul(2));
        let a = fetch32(input, 0) as u64;
        return hash_len_16_mul(
            (len as u64).wrapping_add(a << 3),
            fetch32(input, len - 4) as u64,
            mul,
        );
    }
    if len > 0 {
        let a = input[0] as u64;
        let b = input[len >> 1] as u64;
        let c = input[len - 1] as u64;
        let y = a.wrapping_add(b << 8);
        let z = (len as u64).wrapping_add(c << 2);
        return shift_mix(y.wrapping_mul(K2) ^ z.wrapping_mul(K0)).wrapping_mul(K2);
    }
    K2
}

fn hash_len_17_to_32(input: &[u8]) -> u64 {
    let len = input.len();
    let mul = K2.wrapping_add((len as u64).wrapping_mul(2));
    let a = fetch64(input, 0).wrapping_mul(K1);
    let b = fetch64(input, 8);
    let c = fetch64(input, len - 8).wrapping_mul(mul);
    let d = fetch64(input, len - 16).wrapping_mul(K2);
    hash_len_16_mul(
        rotate(a.wrapping_add(b), 43)
            .wrapping_add(rotate(c, 30))
            .wrapping_add(d),
        a.wrapping_add(rotate(b.wrapping_add(K2), 18))
            .wrapping_add(c),
        mul,
    )
}

fn hash_len_33_to_64(input: &[u8]) -> u64 {
    let len = input.len();
    let mul = K2.wrapping_add((len as u64).wrapping_mul(2));
    let mut a = fetch64(input, 0).wrapping_mul(K2);
    let mut b = fetch64(input, 8);
    let c = fetch64(input, len - 24);
    let d = fetch64(input, len - 32);
    let e = fetch64(input, 16).wrapping_mul(K2);
    let f = fetch64(input, 24).wrapping_mul(9);
    let g = fetch64(input, len - 8);
    let h = fetch64(input, len - 16).wrapping_mul(mul);
    let u = rotate(a.wrapping_add(g), 43)
        .wrapping_add(rotate(b, 30).wrapping_add(c).wrapping_mul(9));
    let v = a.wrapping_add(g) ^ d;
    let v = v.wrapping_add(f).wrapping_add(1);
    let w = u.wrapping_add(v).wrapping_mul(mul).swap_bytes().wrapping_add(h);
    let x = rotate(e.wrapping_add(f), 42).wrapping_add(c);
    let y = v
        .wrapping_add(w)
        .wrapping_mul(mul)
        .swap_bytes()
        .wrapping_add(g)
        .wrapping_mul(mul);
    let z = e.wrapping_add(f).wrapping_add(c);
    a = x
        .wrapping_add(z)
        .wrapping_mul(mul)
        .wrapping_add(y)
        .swap_bytes()
        .wrapping_add(b);
    b = shift_mix(
        z.wrapping_add(a)
            .wrapping_mul(mul)
            .wrapping_add(d)
            .wrapping_add(h),
    )
    .wrapping_mul(mul);
    b.wrapping_add(x)
}

fn weak_hash_len_32_with_seeds(input: &[u8], a: u64, b: u64) -> (u64, u64) {
    let w = fetch64(input, 0);
    let x = fetch64(input, 8);
    let y = fetch64(input, 16);
    let z = fetch64(input, 24);
    let a = a.wrapping_add(w);
    let mut b = rotate(b.wrapping_add(a).wrapping_add(z), 21);
    let c = a;
    let a = a.wrapping_add(x).wrapping_add(y);
    b = b.wrapping_add(rotate(a, 44));
    (a.wrapping_add(z), b.wrapping_add(c))
}

fn city_hash64(input: &[u8]) -> u64 {
    let len = input.len();
    if len <= 16 {
        return hash_len_0_to_16(input);
    }
    if len <= 32 {
        return hash_len_17_to_32(input);
    }
    if len <= 64 {
        return hash_len_33_to_64(input);
    }

    let mut x = fetch64(input, len - 40);
    let mut y = fetch64(input, len - 16).wrapping_add(fetch64(input, len - 56));
    let mut z = hash_len_16(
        fetch64(input, len - 48).wrapping_add(len as u64),
        fetch64(input, len - 24),
    );
    let mut v = weak_hash_len_32_with_seeds(&input[len - 64..], len as u64, z);
    let mut w = weak_hash_len_32_with_seeds(&input[len - 32..], y.wrapping_add(K1), x);
    x = x.wrapping_mul(K1).wrapping_add(fetch64(input, 0));
    let mut offset = 0usize;
    let mut remaining = (len - 1) & !63;
    while remaining != 0 {
        x = rotate(
            x.wrapping_add(y)
                .wrapping_add(v.0)
                .wrapping_add(fetch64(input, offset + 8)),
            37,
        )
        .wrapping_mul(K1);
        y = rotate(
            y.wrapping_add(v.1)
                .wrapping_add(fetch64(input, offset + 48)),
            42,
        )
        .wrapping_mul(K1);
        x ^= w.1;
        y = y.wrapping_add(v.0).wrapping_add(fetch64(input, offset + 40));
        z = rotate(z.wrapping_add(w.0), 33).wrapping_mul(K1);
        v = weak_hash_len_32_with_seeds(
            &input[offset..offset + 32],
            v.1.wrapping_mul(K1),
            x.wrapping_add(w.0),
        );
        w = weak_hash_len_32_with_seeds(
            &input[offset + 32..offset + 64],
            z.wrapping_add(w.1),
            y.wrapping_add(fetch64(input, offset + 16)),
        );
        std::mem::swap(&mut z, &mut x);
        offset += 64;
        remaining -= 64;
    }
    hash_len_16(
        hash_len_16(v.0, w.0)
            .wrapping_add(shift_mix(y).wrapping_mul(K1))
            .wrapping_add(z),
        hash_len_16(v.1, w.1).wrapping_add(x),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn derives_palworld_uid_from_steam_id() {
        assert_eq!(
            steam_id_to_player_uid("76561198000000000").unwrap(),
            "a5d406b9-0000-0000-0000-000000000000"
        );
    }

    #[test]
    fn rejects_invalid_steam_id() {
        assert!(matches!(
            steam_id_to_player_uid("not-a-steam-id"),
            Err(HostMigrationError::InvalidSteamId)
        ));
    }

    #[test]
    fn plan_blocks_existing_target() {
        let root = tempdir().unwrap();
        fs::write(root.path().join("Level.sav"), b"fixture").unwrap();
        let players = root.path().join("Players");
        fs::create_dir(&players).unwrap();
        fs::write(players.join(player_file_name(SOURCE_HOST_UID)), b"source").unwrap();
        let target = steam_id_to_player_uid("76561198000000000").unwrap();
        fs::write(players.join(player_file_name(&target)), b"target").unwrap();
        let plan = analyze_host_migration(root.path(), "76561198000000000").unwrap();
        assert_eq!(plan.strategy, "target_exists");
        assert!(!plan.can_execute);
        assert!(plan.target_player_exists);
    }
}

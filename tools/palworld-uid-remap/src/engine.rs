use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::{BufReader, Cursor, Read, Write};
use std::path::{Component, Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use serde::Serialize;
use sha2::{Digest, Sha256};
use thiserror::Error;
use uesave::games::palworld::{palworld_types, Palworld};
use uesave::{Save, SaveReader};

use crate::{
    rewrite_typed_tree, visitor::scan_opaque_bytes, FieldCategory, MappingSet, OpaqueCandidate,
    RewriteReport,
};

static STAGE_SEQUENCE: AtomicU64 = AtomicU64::new(0);

#[cfg(test)]
#[path = "engine_tests.rs"]
mod tests;

pub struct RemapOptions {
    #[cfg(test)]
    before_validation: Option<Box<dyn Fn(&Path)>>,
    #[cfg(test)]
    before_second_manifest: Option<Box<dyn Fn(&Path)>>,
}

impl Default for RemapOptions {
    fn default() -> Self {
        Self {
            #[cfg(test)]
            before_validation: None,
            #[cfg(test)]
            before_second_manifest: None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct VerificationReport {
    pub mappings: Vec<(String, String)>,
    pub input_manifest_sha256: String,
    pub output_manifest_sha256: String,
    pub rewritten_files: Vec<String>,
    pub rewritten_fields: BTreeMap<FieldCategory, u64>,
    pub semantic_fingerprints: BTreeMap<String, String>,
    pub opaque_candidates: Vec<OpaqueCandidate>,
    pub warnings: Vec<String>,
}

#[derive(Debug, Error)]
pub enum RemapError {
    #[error("output already exists: {0}")]
    OutputExists(PathBuf),
    #[error("input world is invalid: {0}")]
    InvalidInput(String),
    #[error("unsafe input entry: {0}")]
    UnsafeEntry(PathBuf),
    #[error("case-folding path collision: {0}")]
    CaseCollision(String),
    #[error("non-canonical player file: {0}")]
    NonCanonicalPlayerFile(PathBuf),
    #[error("source player file is missing: {0}")]
    SourcePlayerMissing(PathBuf),
    #[error("target player file already exists: {0}")]
    TargetPlayerExists(PathBuf),
    #[error("save parse failed for {path}: {message}")]
    Parse { path: PathBuf, message: String },
    #[error("I/O failed for {path}: {source}")]
    Io {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("verification gate failed: {0}")]
    Gate(String),
    #[error("opaque source UID reference in {file} at {path}")]
    OpaqueSourceReference { file: String, path: String },
    #[error("opaque target UID reference in {file} at {path}")]
    OpaqueTargetReference { file: String, path: String },
    #[error("input changed while remapping")]
    InputChanged,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
enum EntryKind {
    Directory,
    File,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ManifestEntry {
    kind: EntryKind,
    size: u64,
    sha256: Option<String>,
}

type Manifest = BTreeMap<String, ManifestEntry>;

pub fn remap_world(
    input_dir: impl AsRef<Path>,
    output_dir: impl AsRef<Path>,
    mapping: &MappingSet,
    options: &RemapOptions,
) -> Result<VerificationReport, RemapError> {
    let input_dir = input_dir.as_ref();
    let output_dir = output_dir.as_ref();
    let _ = options;
    if output_dir.exists() {
        return Err(RemapError::OutputExists(output_dir.to_path_buf()));
    }

    reject_reparse_path_chain(input_dir)?;
    let input = fs::canonicalize(input_dir).map_err(|source| RemapError::Io {
        path: input_dir.to_path_buf(),
        source,
    })?;
    let output_parent = output_dir.parent().ok_or_else(|| {
        RemapError::InvalidInput("output must have a parent directory".to_owned())
    })?;
    reject_reparse_path_chain(output_parent)?;
    let output_parent = fs::canonicalize(output_parent).map_err(|source| RemapError::Io {
        path: output_parent.to_path_buf(),
        source,
    })?;
    let output_name = output_dir.file_name().ok_or_else(|| {
        RemapError::InvalidInput("output must name a directory".to_owned())
    })?;
    let output = output_parent.join(output_name);
    if output.starts_with(&input) {
        return Err(RemapError::InvalidInput(
            "output must not be inside input".to_owned(),
        ));
    }

    let manifest = build_manifest(&input)?;
    let level = input.join("Level.sav");
    let players = input.join("Players");
    if !manifest.contains_key("Level.sav") || !players.is_dir() {
        return Err(RemapError::InvalidInput(
            "world root must contain Level.sav and Players/".to_owned(),
        ));
    }
    validate_player_names(&players)?;
    validate_mapping_files(&players, mapping)?;

    let manifest_hash = hash_manifest(&manifest);
    let mut stage = StageGuard::create(&output)?;
    copy_tree(&input, stage.path())?;

    let mut rewritten_files = Vec::new();
    let mut warnings = Vec::new();
    let mut rewritten_fields = BTreeMap::new();
    let mut before_source_counts = CountByUid::new();
    let mut before_target_counts = CountByUid::new();
    let mut semantic_before = BTreeMap::new();

    let mut level_save = parse_save(&level)?;
    semantic_before.insert(
        "Level.sav".to_owned(),
        semantic_fingerprint(&level_save, mapping)?,
    );
    collect_target_baseline(
        &level_save,
        mapping,
        "Level.sav",
        &mut before_target_counts,
    )?;
    let level_report = rewrite_typed_tree(&mut level_save.root.properties, mapping, "Level.sav");
    reject_opaque_candidates(&level_report)?;
    let ignored_custom_version_candidates = level_report
        .opaque_candidates
        .iter()
        .filter(|candidate| is_ignorable_host_custom_version_candidate(candidate))
        .count();
    if ignored_custom_version_candidates != 0 {
        warnings.push(format!(
            concat!(
                "preserved {} host UID-like byte sequence(s) ",
                "in Level.sav ItemContainerSaveData CustomVersionData metadata"
            ),
            ignored_custom_version_candidates
        ));
    }
    for (source, _) in mapping.pairs() {
        let count = level_report
            .rewritten_by_source
            .get(&source.to_string())
            .and_then(|counts| counts.get(&FieldCategory::CharacterIndex))
            .copied()
            .unwrap_or(0);
        if count == 0 {
            return Err(RemapError::Gate(format!(
                "source UID {} is absent from parsed player index",
                source
            )));
        }
    }
    merge_rewrite_report(
        &level_report,
        &mut rewritten_fields,
        &mut before_source_counts,
    );
    write_save_new(&level_save, &stage.path().join("Level.sav"))?;
    rewritten_files.push("Level.sav".to_owned());

    let players_stage = stage.path().join("Players");
    let players_input = input.join("Players");
    for (source, target) in mapping.pairs() {
        rewrite_player_file(
            &players_input,
            &players_stage,
            source,
            target,
            false,
            mapping,
            &mut rewritten_files,
            &mut rewritten_fields,
            &mut before_source_counts,
            &mut before_target_counts,
            &mut semantic_before,
        )?;
        let source_dps = players_input.join(dps_file_name(source));
        if source_dps.is_file() {
            rewrite_player_file(
                &players_input,
                &players_stage,
                source,
                target,
                true,
                mapping,
                &mut rewritten_files,
                &mut rewritten_fields,
                &mut before_source_counts,
                &mut before_target_counts,
                &mut semantic_before,
            )?;
        }
    }

    collect_unrelated_baseline(
        &input,
        &manifest,
        mapping,
        &mut before_target_counts,
        &mut semantic_before,
    )?;
    reject_typed_target_baseline(&before_target_counts)?;

    #[cfg(test)]
    if let Some(hook) = &options.before_validation {
        hook(stage.path());
    }
    let stage_manifest = build_manifest(stage.path())?;
    verify_unrelated(&manifest, &stage_manifest, mapping)?;
    verify_names(&players_stage, mapping)?;

    let mut after_target_counts = CountByUid::new();
    let mut opaque_candidates = Vec::new();
    let mut semantic_after = BTreeMap::new();
    for relative in sav_files(&stage_manifest) {
        let path = manifest_path(stage.path(), &relative);
        let save = parse_save(&path)?;
        reject_opaque_candidates(&scan_opaque_bytes(
            &save.extra,
            mapping,
            &relative,
            "<gvas-extra>",
        ))?;
        let mut source_probe = save.root.properties.clone();
        let source_report = rewrite_typed_tree(&mut source_probe, mapping, &relative);
        reject_opaque_candidates(&source_report)?;
        let ignored_map_mask_candidates = source_report
            .opaque_candidates
            .iter()
            .filter(|candidate| is_ignorable_local_map_mask_candidate(candidate))
            .count();
        if ignored_map_mask_candidates != 0 {
            warnings.push(format!(
                concat!(
                    "preserved {} UID-like byte sequence(s) in LocalData.sav ",
                    "world map mask texture data"
                ),
                ignored_map_mask_candidates
            ));
        }
        if !source_report.rewritten_fields.is_empty() {
            return Err(RemapError::Gate(format!(
                "source UID typed reference remains in {relative}"
            )));
        }
        let mut target_probe = save.root.properties.clone();
        let target_report = rewrite_typed_tree(&mut target_probe, &mapping.reversed(), &relative);
        merge_counts(&mut after_target_counts, &target_report.rewritten_by_source);
        opaque_candidates.extend(source_report.opaque_candidates);
        semantic_after.insert(relative.clone(), semantic_fingerprint(&save, mapping)?);
        verify_normalized_roundtrip(&save, mapping, &relative)?;
    }

    verify_reference_transfer(
        mapping,
        &before_source_counts,
        &after_target_counts,
    )?;
    verify_player_save_values(stage.path(), mapping)?;
    verify_semantic_fingerprints(&semantic_before, &semantic_after)?;

    if output.exists() {
        return Err(RemapError::OutputExists(output));
    }
    #[cfg(test)]
    if let Some(hook) = &options.before_second_manifest {
        hook(&input);
    }
    let second_manifest = build_manifest(&input)?;
    if second_manifest != manifest {
        return Err(RemapError::InputChanged);
    }
    let output_manifest_sha256 = hash_manifest(&stage_manifest);
    fs::rename(stage.path(), &output).map_err(|source| RemapError::Io {
        path: output.clone(),
        source,
    })?;
    stage.disarm();

    Ok(VerificationReport {
        mappings: mapping.canonical_pairs(),
        input_manifest_sha256: manifest_hash,
        output_manifest_sha256,
        rewritten_files,
        rewritten_fields,
        semantic_fingerprints: semantic_after,
        opaque_candidates,
        warnings,
    })
}

type CountByUid = BTreeMap<String, BTreeMap<FieldCategory, u64>>;

fn rewrite_player_file(
    input_players: &Path,
    stage_players: &Path,
    source: &uesave::FGuid,
    target: &uesave::FGuid,
    dps: bool,
    mapping: &MappingSet,
    rewritten_files: &mut Vec<String>,
    rewritten_fields: &mut BTreeMap<FieldCategory, u64>,
    before_source_counts: &mut CountByUid,
    before_target_counts: &mut CountByUid,
    semantic_before: &mut BTreeMap<String, String>,
) -> Result<(), RemapError> {
    let source_name = if dps {
        dps_file_name(source)
    } else {
        player_file_name(source)
    };
    let target_name = if dps {
        dps_file_name(target)
    } else {
        player_file_name(target)
    };
    let relative_source = format!("Players/{source_name}");
    let relative_target = format!("Players/{target_name}");
    let mut save = parse_save(&input_players.join(&source_name))?;
    semantic_before.insert(relative_target.clone(), semantic_fingerprint(&save, mapping)?);
    collect_target_baseline(&save, mapping, &relative_source, before_target_counts)?;
    let report = rewrite_typed_tree(&mut save.root.properties, mapping, &relative_source);
    reject_opaque_candidates(&report)?;
    if report
        .rewritten_by_source
        .get(&source.to_string())
        .and_then(|counts| counts.get(&FieldCategory::PlayerSave))
        .copied()
        .unwrap_or(0)
        != 2
    {
        return Err(RemapError::Gate(format!(
            "{relative_source} must contain exactly two player SaveData UID fields"
        )));
    }
    merge_rewrite_report(&report, rewritten_fields, before_source_counts);
    let old_stage = stage_players.join(&source_name);
    let new_stage = stage_players.join(&target_name);
    write_save_create_new(&save, &new_stage)?;
    fs::remove_file(&old_stage).map_err(|source| RemapError::Io {
        path: old_stage,
        source,
    })?;
    rewritten_files.push(relative_target);
    Ok(())
}

fn collect_target_baseline(
    save: &Save<Palworld>,
    mapping: &MappingSet,
    file: &str,
    counts: &mut CountByUid,
) -> Result<(), RemapError> {
    reject_opaque_candidates(&scan_opaque_bytes(
        &save.extra,
        mapping,
        file,
        "<gvas-extra>",
    ))?;
    let mut opaque_probe = save.root.properties.clone();
    let opaque_report = rewrite_typed_tree(&mut opaque_probe, mapping, file);
    reject_opaque_candidates(&opaque_report)?;
    let mut probe = save.root.properties.clone();
    let report = rewrite_typed_tree(&mut probe, &mapping.reversed(), file);
    merge_counts(counts, &report.rewritten_by_source);
    Ok(())
}

fn collect_unrelated_baseline(
    input: &Path,
    manifest: &Manifest,
    mapping: &MappingSet,
    target_counts: &mut CountByUid,
    semantic: &mut BTreeMap<String, String>,
) -> Result<(), RemapError> {
    for relative in sav_files(manifest) {
        if relative == "Level.sav" || mapped_source_relative(&relative, mapping) {
            continue;
        }
        let save = parse_save(&manifest_path(input, &relative))?;
        collect_target_baseline(&save, mapping, &relative, target_counts)?;
        semantic.insert(relative, semantic_fingerprint(&save, mapping)?);
    }
    Ok(())
}

fn mapped_source_relative(relative: &str, mapping: &MappingSet) -> bool {
    mapping.pairs().any(|(source, _)| {
        relative == format!("Players/{}", player_file_name(source))
            || relative == format!("Players/{}", dps_file_name(source))
    })
}

fn merge_rewrite_report(
    report: &RewriteReport,
    totals: &mut BTreeMap<FieldCategory, u64>,
    by_uid: &mut CountByUid,
) {
    for (category, count) in &report.rewritten_fields {
        *totals.entry(*category).or_default() += count;
    }
    merge_counts(by_uid, &report.rewritten_by_source);
}

fn merge_counts(target: &mut CountByUid, source: &CountByUid) {
    for (uid, categories) in source {
        for (category, count) in categories {
            *target
                .entry(uid.clone())
                .or_default()
                .entry(*category)
                .or_default() += count;
        }
    }
}

fn reject_opaque_candidates(report: &RewriteReport) -> Result<(), RemapError> {
    if let Some(candidate) = report
        .opaque_candidates
        .iter()
        .find(|candidate| !is_ignorable_opaque_candidate(candidate))
    {
        return match candidate.kind {
            crate::CandidateKind::Source => Err(RemapError::OpaqueSourceReference {
                file: candidate.file.clone(),
                path: candidate.path.clone(),
            }),
            crate::CandidateKind::Target => Err(RemapError::OpaqueTargetReference {
                file: candidate.file.clone(),
                path: candidate.path.clone(),
            }),
        };
    }
    Ok(())
}

fn is_ignorable_opaque_candidate(candidate: &OpaqueCandidate) -> bool {
    is_ignorable_host_custom_version_candidate(candidate)
        || is_ignorable_local_map_mask_candidate(candidate)
}

fn is_ignorable_local_map_mask_candidate(candidate: &OpaqueCandidate) -> bool {
    if candidate.file.replace('\\', "/") != "LocalData.sav" {
        return false;
    }

    let Some(version) = candidate
        .path
        .strip_prefix("SaveData.WorldMapMaskTexture")
    else {
        return false;
    };

    version.is_empty()
        || version.strip_prefix('V').is_some_and(|digits| {
            !digits.is_empty() && digits.bytes().all(|byte| byte.is_ascii_digit())
        })
}

fn is_ignorable_host_custom_version_candidate(candidate: &OpaqueCandidate) -> bool {
    if candidate.file != "Level.sav"
        || candidate.kind != crate::CandidateKind::Source
        || candidate.uid.as_str() != crate::host::SOURCE_HOST_UID
    {
        return false;
    }

    let Some(index) = candidate
        .path
        .strip_prefix("worldSaveData.ItemContainerSaveData[")
        .and_then(|path| path.strip_suffix("].Value.CustomVersionData"))
    else {
        return false;
    };

    !index.is_empty() && index.bytes().all(|byte| byte.is_ascii_digit())
}

fn reject_typed_target_baseline(target_baseline: &CountByUid) -> Result<(), RemapError> {
    if let Some((target, categories)) = target_baseline
        .iter()
        .find(|(_, categories)| categories.values().any(|count| *count != 0))
    {
        return Err(RemapError::Gate(format!(
            "target UID typed reference already exists in input for {target}: {categories:?}"
        )));
    }
    Ok(())
}

fn verify_reference_transfer(
    mapping: &MappingSet,
    source_counts: &CountByUid,
    target_after: &CountByUid,
) -> Result<(), RemapError> {
    for (source, target) in mapping.pairs() {
        let source_values = source_counts.get(&source.to_string()).cloned().unwrap_or_default();
        let expected = source_values;
        let actual = target_after
            .get(&target.to_string())
            .cloned()
            .unwrap_or_default();
        if actual != expected {
            return Err(RemapError::Gate(format!(
                "typed reference transfer mismatch for {source} -> {target}"
            )));
        }
    }
    Ok(())
}

fn verify_player_save_values(root: &Path, mapping: &MappingSet) -> Result<(), RemapError> {
    for (_, target) in mapping.pairs() {
        for name in [player_file_name(target), dps_file_name(target)] {
            let path = root.join("Players").join(&name);
            if !path.exists() {
                if name.ends_with("_dps.sav") {
                    continue;
                }
                return Err(RemapError::Gate(format!("target player file is absent: {name}")));
            }
            let save = parse_save(&path)?;
            let save_data = struct_properties(
                save.root
                    .properties
                    .0
                    .get(&uesave::PropertyKey::from("SaveData")),
                "SaveData",
            )?;
            let direct = property_guid(
                save_data
                    .0
                    .get(&uesave::PropertyKey::from("PlayerUId")),
            )?;
            let individual = struct_properties(
                save_data
                    .0
                    .get(&uesave::PropertyKey::from("IndividualId")),
                "SaveData.IndividualId",
            )?;
            let nested = property_guid(
                individual
                    .0
                    .get(&uesave::PropertyKey::from("PlayerUId")),
            )?;
            if direct != *target || nested != *target {
                return Err(RemapError::Gate(format!(
                    "target UID fields mismatch in Players/{name}"
                )));
            }
        }
    }
    Ok(())
}

fn struct_properties<'a>(
    property: Option<&'a uesave::Property<uesave::SaveGameArchiveType<Palworld>>>,
    path: &str,
) -> Result<&'a uesave::Properties<uesave::SaveGameArchiveType<Palworld>>, RemapError> {
    match property {
        Some(uesave::Property::Struct(uesave::StructValue::Struct(properties))) => Ok(properties),
        _ => Err(RemapError::Gate(format!("missing typed struct {path}"))),
    }
}

fn property_guid(
    property: Option<&uesave::Property<uesave::SaveGameArchiveType<Palworld>>>,
) -> Result<uesave::FGuid, RemapError> {
    match property {
        Some(uesave::Property::Struct(uesave::StructValue::Guid(uid))) => Ok(*uid),
        _ => Err(RemapError::Gate("missing typed player UID".to_owned())),
    }
}

fn semantic_fingerprint(
    save: &Save<Palworld>,
    mapping: &MappingSet,
) -> Result<String, RemapError> {
    let mut json = serde_json::to_string(&save.root).map_err(|error| {
        RemapError::Gate(format!("semantic serialization failed: {error}"))
    })?;
    for (index, (source, target)) in mapping.pairs().enumerate() {
        let placeholder = format!("<mapped-player-{index:04}>");
        json = json.replace(&source.to_string(), &placeholder);
        json = json.replace(&target.to_string(), &placeholder);
    }
    let mut hasher = Sha256::new();
    hasher.update(json.as_bytes());
    hasher.update([0]);
    hasher.update(&save.extra);
    Ok(format!("{:x}", hasher.finalize()))
}

fn verify_semantic_fingerprints(
    before: &BTreeMap<String, String>,
    after: &BTreeMap<String, String>,
) -> Result<(), RemapError> {
    if before != after {
        return Err(RemapError::Gate(
            "semantic invariant fingerprint changed".to_owned(),
        ));
    }
    Ok(())
}

fn verify_normalized_roundtrip(
    save: &Save<Palworld>,
    mapping: &MappingSet,
    relative: &str,
) -> Result<(), RemapError> {
    let expected = semantic_fingerprint(save, mapping)?;
    let mut first = Vec::new();
    save.write_compressed(&mut first)
        .map_err(|error| RemapError::Gate(format!("first write failed for {relative}: {error}")))?;
    let reparsed = parse_save_reader(Cursor::new(first), relative)?;
    let mut second = Vec::new();
    reparsed.write_compressed(&mut second).map_err(|error| {
        RemapError::Gate(format!("second write failed for {relative}: {error}"))
    })?;
    let second_parse = parse_save_reader(Cursor::new(second), relative)?;
    if semantic_fingerprint(&reparsed, mapping)? != expected
        || semantic_fingerprint(&second_parse, mapping)? != expected
    {
        return Err(RemapError::Gate(format!(
            "normalized round-trip changed semantics for {relative}"
        )));
    }
    Ok(())
}

fn parse_save_reader<R: Read>(reader: R, relative: &str) -> Result<Save<Palworld>, RemapError> {
    SaveReader::new()
        .types(palworld_types())
        .game::<Palworld>()
        .error_to_raw(false)
        .read(reader)
        .map_err(|error| RemapError::Parse {
            path: PathBuf::from(relative),
            message: error.to_string(),
        })
}

fn write_save_new(save: &Save<Palworld>, destination: &Path) -> Result<(), RemapError> {
    let temporary = destination.with_extension("sav.palpanel-new");
    write_save_create_new(save, &temporary)?;
    fs::remove_file(destination).map_err(|source| RemapError::Io {
        path: destination.to_path_buf(),
        source,
    })?;
    fs::rename(&temporary, destination).map_err(|source| RemapError::Io {
        path: destination.to_path_buf(),
        source,
    })
}

fn write_save_create_new(save: &Save<Palworld>, path: &Path) -> Result<(), RemapError> {
    let mut bytes = Vec::new();
    save.write_compressed(&mut bytes)
        .map_err(|error| RemapError::Gate(format!("save write failed for {}: {error}", path.display())))?;
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)
        .map_err(|source| RemapError::Io {
            path: path.to_path_buf(),
            source,
        })?;
    file.write_all(&bytes).map_err(|source| RemapError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    file.sync_all().map_err(|source| RemapError::Io {
        path: path.to_path_buf(),
        source,
    })
}

fn copy_tree(source: &Path, destination: &Path) -> Result<(), RemapError> {
    for entry in fs::read_dir(source).map_err(|source_error| RemapError::Io {
        path: source.to_path_buf(),
        source: source_error,
    })? {
        let entry = entry.map_err(|source_error| RemapError::Io {
            path: source.to_path_buf(),
            source: source_error,
        })?;
        let from = entry.path();
        let to = destination.join(entry.file_name());
        let kind = entry.file_type().map_err(|source_error| RemapError::Io {
            path: from.clone(),
            source: source_error,
        })?;
        if kind.is_dir() {
            fs::create_dir(&to).map_err(|source_error| RemapError::Io {
                path: to.clone(),
                source: source_error,
            })?;
            copy_tree(&from, &to)?;
        } else if kind.is_file() {
            fs::copy(&from, &to).map_err(|source_error| RemapError::Io {
                path: to.clone(),
                source: source_error,
            })?;
            OpenOptions::new()
                .write(true)
                .open(&to)
                .and_then(|file| file.sync_all())
                .map_err(|source_error| RemapError::Io {
                    path: to,
                    source: source_error,
                })?;
        } else {
            return Err(RemapError::UnsafeEntry(from));
        }
    }
    Ok(())
}

fn verify_unrelated(
    input: &Manifest,
    output: &Manifest,
    mapping: &MappingSet,
) -> Result<(), RemapError> {
    for (relative, entry) in input {
        if entry.kind != EntryKind::File || relative == "Level.sav" || mapped_source_relative(relative, mapping) {
            continue;
        }
        if output.get(relative) != Some(entry) {
            return Err(RemapError::Gate(format!(
                "unrelated file changed: {relative}"
            )));
        }
    }
    Ok(())
}

fn verify_names(players: &Path, mapping: &MappingSet) -> Result<(), RemapError> {
    for (source, target) in mapping.pairs() {
        if players.join(player_file_name(source)).exists()
            || players.join(dps_file_name(source)).exists()
        {
            return Err(RemapError::Gate(format!(
                "old player file remains for {source}"
            )));
        }
        if !players.join(player_file_name(target)).is_file() {
            return Err(RemapError::Gate(format!(
                "target player file missing for {target}"
            )));
        }
    }
    Ok(())
}

fn manifest_path(root: &Path, relative: &str) -> PathBuf {
    relative
        .split('/')
        .fold(root.to_path_buf(), |path, component| path.join(component))
}

fn sav_files(manifest: &Manifest) -> Vec<String> {
    manifest
        .iter()
        .filter(|(path, entry)| entry.kind == EntryKind::File && path.ends_with(".sav"))
        .map(|(path, _)| path.clone())
        .collect()
}

fn hash_manifest(manifest: &Manifest) -> String {
    let mut hasher = Sha256::new();
    for (path, entry) in manifest {
        hasher.update(path.as_bytes());
        hasher.update([0]);
        hasher.update(match entry.kind {
            EntryKind::Directory => b"directory".as_slice(),
            EntryKind::File => b"file".as_slice(),
        });
        hasher.update(entry.size.to_le_bytes());
        if let Some(hash) = &entry.sha256 {
            hasher.update(hash.as_bytes());
        }
        hasher.update([0xff]);
    }
    format!("{:x}", hasher.finalize())
}

struct StageGuard {
    path: PathBuf,
    parent: PathBuf,
    prefix: String,
    active: bool,
}

impl StageGuard {
    fn create(output: &Path) -> Result<Self, RemapError> {
        let parent = output
            .parent()
            .ok_or_else(|| RemapError::InvalidInput("output has no parent".to_owned()))?
            .to_path_buf();
        let name = output
            .file_name()
            .ok_or_else(|| RemapError::InvalidInput("output has no name".to_owned()))?
            .to_string_lossy();
        let prefix = format!(".{name}.palworld-uid-remap-stage-");
        for _ in 0..100 {
            let sequence = STAGE_SEQUENCE.fetch_add(1, Ordering::Relaxed);
            let candidate = parent.join(format!("{prefix}{}-{sequence}", std::process::id()));
            match fs::create_dir(&candidate) {
                Ok(()) => {
                    return Ok(Self {
                        path: candidate,
                        parent,
                        prefix,
                        active: true,
                    });
                }
                Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(source) => {
                    return Err(RemapError::Io {
                        path: candidate,
                        source,
                    });
                }
            }
        }
        Err(RemapError::Gate(
            "could not allocate unique staging directory".to_owned(),
        ))
    }

    fn path(&self) -> &Path {
        &self.path
    }

    fn disarm(&mut self) {
        self.active = false;
    }
}

impl Drop for StageGuard {
    fn drop(&mut self) {
        if !self.active {
            return;
        }
        let valid = self.path.parent() == Some(self.parent.as_path())
            && self
                .path
                .file_name()
                .is_some_and(|name| name.to_string_lossy().starts_with(&self.prefix));
        if valid {
            let _ = fs::remove_dir_all(&self.path);
        }
    }
}

fn parse_save(path: &Path) -> Result<Save<Palworld>, RemapError> {
    let file = File::open(path).map_err(|source| RemapError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    SaveReader::new()
        .types(palworld_types())
        .game::<Palworld>()
        .error_to_raw(false)
        .read(BufReader::new(file))
        .map_err(|error| RemapError::Parse {
            path: path.to_path_buf(),
            message: error.to_string(),
        })
}

fn validate_mapping_files(players: &Path, mapping: &MappingSet) -> Result<(), RemapError> {
    for (source, target) in mapping.pairs() {
        let source_path = players.join(player_file_name(source));
        if !source_path.is_file() {
            return Err(RemapError::SourcePlayerMissing(source_path));
        }
        let target_path = players.join(player_file_name(target));
        if target_path.exists() {
            return Err(RemapError::TargetPlayerExists(target_path));
        }
        let target_dps = players.join(dps_file_name(target));
        if target_dps.exists() {
            return Err(RemapError::TargetPlayerExists(target_dps));
        }
    }
    Ok(())
}

fn validate_player_names(players: &Path) -> Result<(), RemapError> {
    let entries = fs::read_dir(players).map_err(|source| RemapError::Io {
        path: players.to_path_buf(),
        source,
    })?;
    for entry in entries {
        let entry = entry.map_err(|source| RemapError::Io {
            path: players.to_path_buf(),
            source,
        })?;
        let path = entry.path();
        if !entry
            .file_type()
            .map_err(|source| RemapError::Io {
                path: path.clone(),
                source,
            })?
            .is_file()
        {
            return Err(RemapError::NonCanonicalPlayerFile(path));
        }
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if !canonical_player_name(&name) {
            return Err(RemapError::NonCanonicalPlayerFile(path));
        }
    }
    Ok(())
}

fn canonical_player_name(name: &str) -> bool {
    let stem = if let Some(stem) = name.strip_suffix("_dps.sav") {
        stem
    } else if let Some(stem) = name.strip_suffix(".sav") {
        stem
    } else {
        return false;
    };
    stem.len() == 32
        && stem
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'A'..=b'F').contains(&byte))
}

fn player_file_name(uid: &uesave::FGuid) -> String {
    format!("{}.sav", uid.to_string().replace('-', "").to_uppercase())
}

fn dps_file_name(uid: &uesave::FGuid) -> String {
    format!(
        "{}_dps.sav",
        uid.to_string().replace('-', "").to_uppercase()
    )
}

fn build_manifest(root: &Path) -> Result<Manifest, RemapError> {
    let root_metadata = fs::symlink_metadata(root).map_err(|source| RemapError::Io {
        path: root.to_path_buf(),
        source,
    })?;
    if !root_metadata.is_dir() || unsafe_metadata(&root_metadata) {
        return Err(RemapError::UnsafeEntry(root.to_path_buf()));
    }

    let mut manifest = Manifest::new();
    let mut folded = BTreeMap::<String, String>::new();
    walk_manifest(root, root, &mut manifest, &mut folded)?;
    Ok(manifest)
}

fn reject_reparse_path_chain(path: &Path) -> Result<(), RemapError> {
    let absolute = if path.is_absolute() {
        path.to_path_buf()
    } else {
        std::env::current_dir()
            .map_err(|source| RemapError::Io {
                path: path.to_path_buf(),
                source,
            })?
            .join(path)
    };
    for ancestor in absolute.ancestors() {
        let metadata = fs::symlink_metadata(ancestor).map_err(|source| RemapError::Io {
            path: ancestor.to_path_buf(),
            source,
        })?;
        if unsafe_metadata(&metadata) {
            return Err(RemapError::UnsafeEntry(ancestor.to_path_buf()));
        }
    }
    Ok(())
}

fn walk_manifest(
    root: &Path,
    directory: &Path,
    manifest: &mut Manifest,
    folded: &mut BTreeMap<String, String>,
) -> Result<(), RemapError> {
    let entries = fs::read_dir(directory).map_err(|source| RemapError::Io {
        path: directory.to_path_buf(),
        source,
    })?;
    for entry in entries {
        let entry = entry.map_err(|source| RemapError::Io {
            path: directory.to_path_buf(),
            source,
        })?;
        let path = entry.path();
        let metadata = fs::symlink_metadata(&path).map_err(|source| RemapError::Io {
            path: path.clone(),
            source,
        })?;
        if unsafe_metadata(&metadata) || (!metadata.is_dir() && !metadata.is_file()) {
            return Err(RemapError::UnsafeEntry(path));
        }
        let relative = path.strip_prefix(root).map_err(|_| {
            RemapError::InvalidInput("input path escaped world root".to_owned())
        })?;
        if relative.components().any(|part| {
            matches!(part, Component::ParentDir | Component::RootDir | Component::Prefix(_))
        }) {
            return Err(RemapError::UnsafeEntry(path));
        }
        let relative = relative.to_string_lossy().replace('\\', "/");
        record_case_path(folded, &relative)?;
        if metadata.is_dir() {
            manifest.insert(
                relative,
                ManifestEntry {
                    kind: EntryKind::Directory,
                    size: 0,
                    sha256: None,
                },
            );
            walk_manifest(root, &path, manifest, folded)?;
        } else {
            manifest.insert(
                relative,
                ManifestEntry {
                    kind: EntryKind::File,
                    size: metadata.len(),
                    sha256: Some(hash_file(&path)?),
                },
            );
        }
    }
    Ok(())
}

fn record_case_path(
    folded: &mut BTreeMap<String, String>,
    relative: &str,
) -> Result<(), RemapError> {
    let case_key = relative.to_lowercase();
    if let Some(existing) = folded.insert(case_key, relative.to_owned()) {
        if existing != relative {
            return Err(RemapError::CaseCollision(format!(
                "{existing} collides with {relative}"
            )));
        }
    }
    Ok(())
}

fn hash_file(path: &Path) -> Result<String, RemapError> {
    let mut file = File::open(path).map_err(|source| RemapError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    let mut hasher = Sha256::new();
    let mut buffer = [0; 64 * 1024];
    loop {
        let read = file.read(&mut buffer).map_err(|source| RemapError::Io {
            path: path.to_path_buf(),
            source,
        })?;
        if read == 0 {
            break;
        }
        hasher.update(&buffer[..read]);
    }
    Ok(format!("{:x}", hasher.finalize()))
}

#[cfg(windows)]
fn unsafe_metadata(metadata: &fs::Metadata) -> bool {
    use std::os::windows::fs::MetadataExt;
    metadata.file_attributes() & 0x400 != 0
}

#[cfg(not(windows))]
fn unsafe_metadata(metadata: &fs::Metadata) -> bool {
    metadata.file_type().is_symlink()
}

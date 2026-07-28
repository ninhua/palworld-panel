//! PalPanel's first-party Palworld UID remapping engine.

use std::collections::{BTreeMap, BTreeSet};

use serde::Deserialize;
use thiserror::Error;
use uesave::FGuid;

mod visitor;
mod engine;
mod host;

pub use engine::{remap_world, RemapError, RemapOptions, VerificationReport};
pub use host::{
    analyze_host_migration, execute_host_migration, steam_id_to_player_uid,
    HostMigrationError, HostMigrationPlan, HostMigrationResult, SOURCE_HOST_UID,
};
pub use visitor::{
    rewrite_typed_tree, CandidateKind, FieldCategory, OpaqueCandidate, RewriteReport,
};

#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum MappingError {
    #[error("mapping must contain at least one entry")]
    Empty,
    #[error("invalid mapping JSON: {0}")]
    InvalidJson(String),
    #[error("UID must be a canonical lowercase GUID: {0}")]
    InvalidUid(String),
    #[error("duplicate source UID: {0}")]
    DuplicateSource(String),
    #[error("duplicate target UID: {0}")]
    DuplicateTarget(String),
    #[error("source and target UID are identical: {0}")]
    SelfMapping(String),
    #[error("UID occurs in both source and target sets: {0}")]
    SourceTargetIntersection(String),
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct MappingEntry {
    source_uid: String,
    target_uid: String,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum MappingInput {
    One(MappingEntry),
    Many(Vec<MappingEntry>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MappingSet {
    pairs: BTreeMap<FGuid, FGuid>,
}

impl MappingSet {
    pub fn from_json(bytes: &[u8]) -> Result<Self, MappingError> {
        let input: MappingInput = serde_json::from_slice(bytes)
            .map_err(|error| MappingError::InvalidJson(error.to_string()))?;
        let entries = match input {
            MappingInput::One(entry) => vec![entry],
            MappingInput::Many(entries) => entries,
        };
        if entries.is_empty() {
            return Err(MappingError::Empty);
        }

        let mut pairs = BTreeMap::new();
        let mut targets = BTreeSet::new();
        for entry in entries {
            let source = parse_canonical_uid(&entry.source_uid)?;
            let target = parse_canonical_uid(&entry.target_uid)?;
            if source == target {
                return Err(MappingError::SelfMapping(source.to_string()));
            }
            if pairs.insert(source, target).is_some() {
                return Err(MappingError::DuplicateSource(source.to_string()));
            }
            if !targets.insert(target) {
                return Err(MappingError::DuplicateTarget(target.to_string()));
            }
        }

        if let Some(uid) = pairs.keys().find(|uid| targets.contains(uid)) {
            return Err(MappingError::SourceTargetIntersection(uid.to_string()));
        }
        Ok(Self { pairs })
    }

    pub fn canonical_pairs(&self) -> Vec<(String, String)> {
        self.pairs
            .iter()
            .map(|(source, target)| (source.to_string(), target.to_string()))
            .collect()
    }

    pub(crate) fn replacement(&self, uid: &FGuid) -> Option<FGuid> {
        self.pairs.get(uid).copied()
    }

    pub(crate) fn pairs(&self) -> impl Iterator<Item = (&FGuid, &FGuid)> {
        self.pairs.iter()
    }

    pub(crate) fn reversed(&self) -> Self {
        Self {
            pairs: self.pairs.iter().map(|(source, target)| (*target, *source)).collect(),
        }
    }
}

fn parse_canonical_uid(value: &str) -> Result<FGuid, MappingError> {
    let parsed = FGuid::parse_str(value).map_err(|_| MappingError::InvalidUid(value.to_owned()))?;
    if parsed.to_string() != value {
        return Err(MappingError::InvalidUid(value.to_owned()));
    }
    Ok(parsed)
}

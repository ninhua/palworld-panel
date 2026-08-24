use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use thiserror::Error;
use uesave::games::palworld::{PalStruct, PalWorkAssign, PalWorkBase, PalWorkTypeSpecificData, Palworld};
use uesave::{
    FGuid, MapEntry, Properties, Property, PropertyKey, Save, SaveGameArchiveType, StructValue,
    ValueVec,
};

use crate::engine::{copy_tree, hash_file, hash_manifest, parse_save, write_save_new, RemapError, StageGuard};

type PalProperties = Properties<SaveGameArchiveType<Palworld>>;
type PalProperty = Property<SaveGameArchiveType<Palworld>>;

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct WorkRequest {
    pub base_camp_id: String,
    pub worker_instance_id: String,
    pub work_base_id: String,
    pub owner_map_object_concrete_model_id: String,
    pub assign_define_data_id: String,
    pub location_index: i32,
    pub expected_level_sha256: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkPlan {
    pub request: WorkRequest,
    pub level_sha256: String,
    pub assignment_id: String,
    pub worker_guid: String,
    pub fixed: u32,
    pub match_count: usize,
}

#[derive(Debug, Serialize)]
pub struct WorkFixResult {
    pub plan: WorkPlan,
    pub changed: bool,
    pub input_manifest_sha256: String,
    pub output_manifest_sha256: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkList {
    pub level_sha256: String,
    pub base_camp_id: String,
    pub work_bases: Vec<WorkListBase>,
    pub assignments: Vec<WorkListAssignment>,
    pub unscoped_assignments: Vec<WorkListUnscopedAssignment>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkListBase {
    pub work_type: String,
    pub base_camp_id: String,
    pub work_base_id: String,
    pub owner_map_object_model_id: String,
    pub owner_map_object_concrete_model_id: String,
    pub map_object_instance_id: Option<String>,
    pub current_state: u8,
    pub assign_location_count: usize,
    pub behaviour_type: u8,
    pub assign_define_data_id: String,
    pub override_work_type: u8,
    pub assignable_fixed_type: u8,
    pub assignable_otomo: u32,
    pub can_trigger_worker_event: u32,
    pub can_steal_assign: u32,
    pub assignment_count: usize,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkListAssignment {
    pub worker_instance_id: String,
    pub work_base_id: String,
    pub owner_map_object_concrete_model_id: String,
    pub assign_define_data_id: String,
    pub location_index: i32,
    pub assignment_id: String,
    pub worker_guid: String,
    pub fixed: u32,
    pub assign_type: u8,
    pub state: u8,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WorkListUnscopedAssignment {
    pub handle_id: String,
    pub location_index: i32,
    pub assign_type: u8,
    pub worker_guid: String,
    pub worker_instance_id: String,
    pub state: u8,
    pub fixed: u32,
    pub map_object_instance_id: Option<String>,
}

#[derive(Debug, Error)]
pub enum WorkError {
    #[error("work request is invalid: {0}")]
    InvalidRequest(String),
    #[error("expected Level.sav SHA256 {expected}, got {actual}")]
    LevelHashMismatch { expected: String, actual: String },
    #[error("no work assignment matches the persistent key")]
    NoRecord,
    #[error("work assignment stable key does not match the existing assignment")]
    StableKeyMismatch,
    #[error("work assignment is ambiguous: {0} matching records")]
    Duplicate(usize),
    #[error("fixed must be 0 or 1, got {0}")]
    InvalidFixed(u32),
    #[error("output already exists: {0}")]
    OutputExists(PathBuf),
    #[error("work verification failed: {0}")]
    Verification(String),
    #[error(transparent)]
    Remap(#[from] RemapError),
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct WorkContext {
    base_camp_id: FGuid,
    work_base_id: FGuid,
    owner_map_object_concrete_model_id: FGuid,
    assign_define_data_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct AssignmentSnapshot {
    context: WorkContext,
    assignment_id: FGuid,
    location_index: i32,
    assign_type: u8,
    worker_guid: FGuid,
    worker_instance_id: FGuid,
    state: u8,
    fixed: u32,
    trailing_bytes: [u8; 4],
    multi_type: Option<(u8, u8, u8, [u8; 4])>,
}

fn parse_guid(name: &str, value: &str) -> Result<FGuid, WorkError> {
    let guid = FGuid::parse_str(value).map_err(|_| WorkError::InvalidRequest(format!("{name} must be a GUID")))?;
    if guid.to_string() != value {
        return Err(WorkError::InvalidRequest(format!("{name} must be canonical lowercase")));
    }
    Ok(guid)
}

fn validate_request(request: &WorkRequest) -> Result<(WorkContext, FGuid), WorkError> {
    if request.expected_level_sha256.len() != 64
        || !request.expected_level_sha256.bytes().all(|b| b.is_ascii_hexdigit())
        || request.expected_level_sha256 != request.expected_level_sha256.to_lowercase()
    {
        return Err(WorkError::InvalidRequest("expected_level_sha256 must be lowercase SHA256".into()));
    }
    Ok((
        WorkContext {
            base_camp_id: parse_guid("base_camp_id", &request.base_camp_id)?,
            work_base_id: parse_guid("work_base_id", &request.work_base_id)?,
            owner_map_object_concrete_model_id: parse_guid(
                "owner_map_object_concrete_model_id",
                &request.owner_map_object_concrete_model_id,
            )?,
            assign_define_data_id: request.assign_define_data_id.clone(),
        },
        parse_guid("worker_instance_id", &request.worker_instance_id)?,
    ))
}

fn snapshot(context: &WorkContext, assignment: &PalWorkAssign) -> AssignmentSnapshot {
    AssignmentSnapshot {
        context: context.clone(),
        assignment_id: assignment.id,
        location_index: assignment.location_index,
        assign_type: assignment.assign_type,
        worker_guid: assignment.assigned_individual_id.guid,
        worker_instance_id: assignment.assigned_individual_id.instance_id,
        state: assignment.state,
        fixed: assignment.fixed,
        trailing_bytes: assignment.trailing_bytes,
        multi_type: assignment.multi_type.as_ref().map(|v| {
            (v.assigned_work_suitability, v.assigned_work_type, v.assigned_work_action_type, v.trailing_bytes)
        }),
    }
}

fn context_from_work(work: &uesave::games::palworld::PalWork) -> Option<WorkContext> {
    let base: &PalWorkBase = work.base_data.as_ref()?;
    Some(WorkContext {
        base_camp_id: base.base_camp_id_belong_to,
        work_base_id: base.id,
        owner_map_object_concrete_model_id: base.owner_map_object_concrete_model_id,
        assign_define_data_id: base.assign_define_data_id.clone(),
    })
}

fn matches_request(request: &WorkRequest, context: &WorkContext, assignment: &PalWorkAssign, worker: &FGuid) -> bool {
    context.base_camp_id.to_string() == request.base_camp_id
        && context.work_base_id.to_string() == request.work_base_id
        && context.owner_map_object_concrete_model_id.to_string() == request.owner_map_object_concrete_model_id
        && context.assign_define_data_id == request.assign_define_data_id
        && assignment.location_index == request.location_index
        && assignment.assigned_individual_id.instance_id == *worker
}

fn persistent_matches(request: &WorkRequest, context: &WorkContext) -> bool {
    context.base_camp_id.to_string() == request.base_camp_id
        && context.work_base_id.to_string() == request.work_base_id
        && context.owner_map_object_concrete_model_id.to_string() == request.owner_map_object_concrete_model_id
        && context.assign_define_data_id == request.assign_define_data_id
}

fn visit_property(property: &mut PalProperty, request: &WorkRequest, worker: &FGuid, records: &mut Vec<AssignmentSnapshot>, mutate: bool) {
    match property {
        Property::Struct(StructValue::Struct(properties)) => visit_properties(properties, request, worker, records, mutate),
        Property::Map(entries) => {
            for MapEntry { key, value } in entries {
                visit_property(key, request, worker, records, mutate);
                visit_property(value, request, worker, records, mutate);
            }
        }
        Property::Array(ValueVec::Struct(values)) | Property::Set(ValueVec::Struct(values)) => {
            for value in values {
                if let StructValue::Struct(properties) = value {
                    visit_properties(properties, request, worker, records, mutate);
                }
            }
        }
        _ => {}
    }
}

fn visit_properties(properties: &mut PalProperties, request: &WorkRequest, worker: &FGuid, records: &mut Vec<AssignmentSnapshot>, mutate: bool) {
    let raw_data_key = PropertyKey::from("RawData");
    let work_assign_map_key = PropertyKey::from("WorkAssignMap");
    let context = properties.0.get(&raw_data_key).and_then(|property| match property {
        Property::Struct(StructValue::Game(PalStruct::Work(work))) => context_from_work(work),
        _ => None,
    });
    if let Some(context) = context {
        if let Some(Property::Map(entries)) = properties.0.get_mut(&work_assign_map_key) {
            for entry in entries {
                let Property::Struct(StructValue::Struct(assign_properties)) = &mut entry.value else { continue };
                let Some(Property::Struct(StructValue::Game(PalStruct::WorkAssign(assignment)))) =
                    assign_properties.0.get_mut(&raw_data_key)
                else {
                    continue;
                };
                let before = snapshot(&context, assignment);
                if persistent_matches(request, &context) && matches_request(request, &context, assignment, worker) && mutate {
                    if assignment.fixed == 0 { assignment.fixed = 1; }
                }
                records.push(if mutate { snapshot(&context, assignment) } else { before });
            }
        }
    }
    for property in properties.0.values_mut() {
        visit_property(property, request, worker, records, mutate);
    }
}

fn all_assignments(save: &mut Save<Palworld>, request: &WorkRequest, worker: &FGuid, mutate: bool) -> Vec<AssignmentSnapshot> {
    let mut records = Vec::new();
    visit_properties(&mut save.root.properties, request, worker, &mut records, mutate);
    records
}

fn collect_all_property(
    property: &mut PalProperty,
    records: &mut Vec<AssignmentSnapshot>,
    work_bases: &mut Vec<WorkListBase>,
    unscoped_assignments: &mut Vec<WorkListUnscopedAssignment>,
) {
    match property {
        Property::Struct(StructValue::Struct(properties)) => {
            collect_all_properties(properties, records, work_bases, unscoped_assignments)
        }
        Property::Map(entries) => {
            for MapEntry { key, value } in entries {
                collect_all_property(key, records, work_bases, unscoped_assignments);
                collect_all_property(value, records, work_bases, unscoped_assignments);
            }
        }
        Property::Array(ValueVec::Struct(values)) | Property::Set(ValueVec::Struct(values)) => {
            for value in values {
                if let StructValue::Struct(properties) = value {
                    collect_all_properties(properties, records, work_bases, unscoped_assignments);
                }
            }
        }
        _ => {}
    }
}

fn collect_all_properties(
    properties: &mut PalProperties,
    records: &mut Vec<AssignmentSnapshot>,
    work_bases: &mut Vec<WorkListBase>,
    unscoped_assignments: &mut Vec<WorkListUnscopedAssignment>,
) {
    let raw_data_key = PropertyKey::from("RawData");
    let work_assign_map_key = PropertyKey::from("WorkAssignMap");
    let assignment_count = match properties.0.get(&work_assign_map_key) {
        Some(Property::Map(entries)) => entries.len(),
        _ => 0,
    };
    let (context, work_base, unscoped) = properties.0.get(&raw_data_key).map_or((None, None, None), |property| match property {
        Property::Struct(StructValue::Game(PalStruct::Work(work))) => {
            let context = context_from_work(work);
            let listed = work.base_data.as_ref().map(|base| WorkListBase {
                work_type: work.work_type.clone(),
                base_camp_id: base.base_camp_id_belong_to.to_string(),
                work_base_id: base.id.to_string(),
                owner_map_object_model_id: base.owner_map_object_model_id.to_string(),
                owner_map_object_concrete_model_id: base.owner_map_object_concrete_model_id.to_string(),
                map_object_instance_id: work.transform.as_ref().and_then(|value| value.map_object_instance_id.as_ref()).map(|value| value.to_string()),
                current_state: base.current_state,
                assign_location_count: base.assign_locations.len(),
                behaviour_type: base.behaviour_type,
                assign_define_data_id: base.assign_define_data_id.clone(),
                override_work_type: base.override_work_type,
                assignable_fixed_type: base.assignable_fixed_type,
                assignable_otomo: base.assignable_otomo,
                can_trigger_worker_event: base.can_trigger_worker_event,
                can_steal_assign: base.can_steal_assign,
                assignment_count,
            });
            let unscoped = match &work.work_specific_data {
                PalWorkTypeSpecificData::Assign {
                    handle_id,
                    location_index,
                    assign_type,
                    assigned_individual_id,
                    state,
                    fixed,
                } => Some(WorkListUnscopedAssignment {
                    handle_id: handle_id.to_string(),
                    location_index: *location_index,
                    assign_type: *assign_type,
                    worker_guid: assigned_individual_id.guid.to_string(),
                    worker_instance_id: assigned_individual_id.instance_id.to_string(),
                    state: *state,
                    fixed: *fixed,
                    map_object_instance_id: work.transform.as_ref().and_then(|value| value.map_object_instance_id.as_ref()).map(|value| value.to_string()),
                }),
                _ => None,
            };
            (context, listed, unscoped)
        }
        _ => (None, None, None),
    });
    if let Some(work_base) = work_base {
        work_bases.push(work_base);
    }
    if let Some(unscoped) = unscoped {
        unscoped_assignments.push(unscoped);
    }
    if let Some(context) = context {
        if let Some(Property::Map(entries)) = properties.0.get_mut(&work_assign_map_key) {
            for entry in entries {
                let Property::Struct(StructValue::Struct(assign_properties)) = &mut entry.value else { continue };
                let Some(Property::Struct(StructValue::Game(PalStruct::WorkAssign(assignment)))) =
                    assign_properties.0.get_mut(&raw_data_key)
                else {
                    continue;
                };
                records.push(snapshot(&context, assignment));
            }
        }
    }
    for property in properties.0.values_mut() {
        collect_all_property(property, records, work_bases, unscoped_assignments);
    }
}

fn all_work_snapshots(
    save: &mut Save<Palworld>,
) -> (Vec<AssignmentSnapshot>, Vec<WorkListBase>, Vec<WorkListUnscopedAssignment>) {
    let mut records = Vec::new();
    let mut work_bases = Vec::new();
    let mut unscoped_assignments = Vec::new();
    collect_all_properties(
        &mut save.root.properties,
        &mut records,
        &mut work_bases,
        &mut unscoped_assignments,
    );
    (records, work_bases, unscoped_assignments)
}

fn list_assignment(record: &AssignmentSnapshot) -> WorkListAssignment {
    WorkListAssignment {
        worker_instance_id: record.worker_instance_id.to_string(),
        work_base_id: record.context.work_base_id.to_string(),
        owner_map_object_concrete_model_id: record.context.owner_map_object_concrete_model_id.to_string(),
        assign_define_data_id: record.context.assign_define_data_id.clone(),
        location_index: record.location_index,
        assignment_id: record.assignment_id.to_string(),
        worker_guid: record.worker_guid.to_string(),
        fixed: record.fixed,
        assign_type: record.assign_type,
        state: record.state,
    }
}

fn sort_work_list_assignments(assignments: &mut [WorkListAssignment]) {
    assignments.sort_by(|left, right| {
        (
            &left.work_base_id,
            left.location_index,
            &left.assignment_id,
            &left.worker_instance_id,
        )
            .cmp(&(
                &right.work_base_id,
                right.location_index,
                &right.assignment_id,
                &right.worker_instance_id,
            ))
    });
}

pub fn analyze_work_list(input_dir: impl AsRef<Path>, base_camp_id: &str) -> Result<WorkList, WorkError> {
    let base_camp = parse_guid("base_camp_id", base_camp_id)?;
    let level = level_path(input_dir.as_ref());
    let level_sha256 = hash_file(&level)?;
    let mut save = parse_save(&level)?;
    let (records, mut work_bases, mut unscoped_assignments) = all_work_snapshots(&mut save);
    let mut assignments: Vec<_> = records
        .into_iter()
        .filter(|record| record.context.base_camp_id == base_camp)
        .map(|record| list_assignment(&record))
        .collect();
    sort_work_list_assignments(&mut assignments);
    work_bases.retain(|work| work.base_camp_id == base_camp_id);
    work_bases.sort_by(|left, right| left.work_base_id.cmp(&right.work_base_id));
    unscoped_assignments.sort_by(|left, right| {
        (&left.handle_id, left.location_index, &left.worker_instance_id)
            .cmp(&(&right.handle_id, right.location_index, &right.worker_instance_id))
    });
    Ok(WorkList {
        level_sha256,
        base_camp_id: base_camp.to_string(),
        work_bases,
        assignments,
        unscoped_assignments,
    })
}

fn select(request: &WorkRequest, records: &[AssignmentSnapshot], worker: &FGuid) -> Result<AssignmentSnapshot, WorkError> {
    let persistent = records.iter().filter(|r| persistent_matches(request, &r.context)).count();
    let matches: Vec<_> = records.iter().filter(|r| {
        persistent_matches(request, &r.context)
            && r.location_index == request.location_index
            && r.worker_instance_id == *worker
    }).cloned().collect();
    if matches.len() > 1 { return Err(WorkError::Duplicate(matches.len())); }
    if let Some(record) = matches.into_iter().next() { return Ok(record); }
    if persistent == 0 { Err(WorkError::NoRecord) } else { Err(WorkError::StableKeyMismatch) }
}

fn level_path(input: &Path) -> PathBuf { input.join("Level.sav") }

fn verify_level_hash(expected: &str, actual: String) -> Result<String, WorkError> {
    if actual != expected {
        return Err(WorkError::LevelHashMismatch { expected: expected.to_owned(), actual });
    }
    Ok(expected.to_owned())
}

pub fn analyze_work_plan(input_dir: impl AsRef<Path>, request: &WorkRequest) -> Result<WorkPlan, WorkError> {
    let input = input_dir.as_ref();
    let (context, worker) = validate_request(request)?;
    let level = level_path(input);
    let actual_hash = hash_file(&level)?;
    verify_level_hash(&request.expected_level_sha256, actual_hash.clone())?;
    let mut save = parse_save(&level)?;
    let records = all_assignments(&mut save, request, &worker, false);
    let record = select(request, &records, &worker)?;
    if record.context != context { return Err(WorkError::StableKeyMismatch); }
    if record.fixed > 1 { return Err(WorkError::InvalidFixed(record.fixed)); }
    Ok(WorkPlan {
        request: request.clone(),
        level_sha256: actual_hash,
        assignment_id: record.assignment_id.to_string(),
        worker_guid: record.worker_guid.to_string(),
        fixed: record.fixed,
        match_count: 1,
    })
}

pub fn execute_work_fix_existing(input_dir: impl AsRef<Path>, output_dir: impl AsRef<Path>, request: &WorkRequest) -> Result<WorkFixResult, WorkError> {
    let input = input_dir.as_ref();
    let output = output_dir.as_ref();
    if output.exists() { return Err(WorkError::OutputExists(output.to_path_buf())); }
    let input_absolute = fs::canonicalize(input).map_err(|source| RemapError::Io { path: input.to_path_buf(), source })?;
    let output_parent = output.parent().ok_or_else(|| WorkError::Verification("output must have a parent directory".into()))?;
    let output_absolute = fs::canonicalize(output_parent)
        .map_err(|source| RemapError::Io { path: output_parent.to_path_buf(), source })?
        .join(output.file_name().ok_or_else(|| WorkError::Verification("output must name a directory".into()))?);
    if output_absolute.starts_with(&input_absolute) {
        return Err(WorkError::Verification("output must not be inside input".into()));
    }
    let plan = analyze_work_plan(input, request)?;
    let manifest = super::engine::build_manifest(input).map_err(WorkError::Remap)?;
    let input_manifest_sha256 = hash_manifest(&manifest);
    let mut stage = StageGuard::create(&output_absolute)?;
    copy_tree(input, stage.path())?;
    let mut save = parse_save(&level_path(input))?;
    let (_, worker) = validate_request(request)?;
    let before = all_assignments(&mut save, request, &worker, false);
    let mut changed = false;
    if plan.fixed == 0 {
        let after = all_assignments(&mut save, request, &worker, true);
        let target_before = select(request, &before, &worker)?;
        let target_after = select(request, &after, &worker)?;
        if target_after.fixed != 1 || target_before.fixed != 0 { return Err(WorkError::Verification("target fixed transition was not exactly 0 to 1".into())); }
        changed = true;
        write_save_new(&save, &stage.path().join("Level.sav"))?;
    }
    let mut output_save = parse_save(&stage.path().join("Level.sav"))?;
    let output_records = all_assignments(&mut output_save, request, &worker, false);
    let target = select(request, &output_records, &worker)?;
    if target.fixed != 1 {
        return Err(WorkError::Verification("output contains an unexpected assignment change".into()));
    }
    let mut before_compare = before.clone();
    let mut adjusted = 0;
    for record in &mut before_compare {
        if record.context == target.context
            && record.assignment_id == target.assignment_id
            && record.location_index == target.location_index
            && record.worker_instance_id == target.worker_instance_id
        {
            record.fixed = 1;
            adjusted += 1;
        }
    }
    if adjusted != 1 || before_compare != output_records {
        return Err(WorkError::Verification("only target fixed may change".into()));
    }
    let stage_manifest = super::engine::build_manifest(stage.path()).map_err(WorkError::Remap)?;
    let second_manifest = super::engine::build_manifest(input).map_err(WorkError::Remap)?;
    if second_manifest != manifest { return Err(WorkError::Verification("input changed while fixing work assignment".into())); }
    let output_manifest_sha256 = hash_manifest(&stage_manifest);
    fs::rename(stage.path(), &output_absolute).map_err(|source| RemapError::Io {
        path: output_absolute.clone(),
        source,
    })?;
    stage.disarm();
    Ok(WorkFixResult { plan, changed, input_manifest_sha256, output_manifest_sha256 })
}

#[cfg(test)]
mod tests {
    use super::*;

    const CAMP: &str = "00112233-4455-6677-8899-aabbccddeeff";
    const WORK: &str = "11112233-4455-6677-8899-aabbccddeeff";
    const OWNER: &str = "21112233-4455-6677-8899-aabbccddeeff";
    const WORKER: &str = "31112233-4455-6677-8899-aabbccddeeff";
    const WORKER_GUID: &str = "41112233-4455-6677-8899-aabbccddeeff";
    const ASSIGN: &str = "51112233-4455-6677-8899-aabbccddeeff";

    fn request() -> WorkRequest {
        WorkRequest {
            base_camp_id: CAMP.into(),
            worker_instance_id: WORKER.into(),
            work_base_id: WORK.into(),
            owner_map_object_concrete_model_id: OWNER.into(),
            assign_define_data_id: "Define_Work".into(),
            location_index: 2,
            expected_level_sha256: "0".repeat(64),
        }
    }

    fn record(fixed: u32, worker: &str, location: i32) -> AssignmentSnapshot {
        AssignmentSnapshot {
            context: WorkContext {
                base_camp_id: FGuid::parse_str(CAMP).unwrap(),
                work_base_id: FGuid::parse_str(WORK).unwrap(),
                owner_map_object_concrete_model_id: FGuid::parse_str(OWNER).unwrap(),
                assign_define_data_id: "Define_Work".into(),
            },
            assignment_id: FGuid::parse_str(ASSIGN).unwrap(),
            location_index: location,
            assign_type: 7,
            worker_guid: FGuid::parse_str(WORKER_GUID).unwrap(),
            worker_instance_id: FGuid::parse_str(worker).unwrap(),
            state: 3,
            fixed,
            trailing_bytes: [9, 8, 7, 6],
            multi_type: None,
        }
    }

    #[test]
    fn selects_existing_assignment_and_supports_idempotent_fixed_one() {
        let request = request();
        let selected = select(&request, &[record(0, WORKER, 2)], &FGuid::parse_str(WORKER).unwrap()).unwrap();
        assert_eq!(selected.fixed, 0);
        let selected = select(&request, &[record(1, WORKER, 2)], &FGuid::parse_str(WORKER).unwrap()).unwrap();
        assert_eq!(selected.fixed, 1);
    }

    #[test]
    fn rejects_no_record_duplicate_and_stable_key_mismatch() {
        let request = request();
        let worker = FGuid::parse_str(WORKER).unwrap();
        assert!(matches!(select(&request, &[], &worker), Err(WorkError::NoRecord)));
        assert!(matches!(select(&request, &[record(0, WORKER, 2), record(1, WORKER, 2)], &worker), Err(WorkError::Duplicate(2))));
        assert!(matches!(select(&request, &[record(0, WORKER, 3)], &worker), Err(WorkError::StableKeyMismatch)));
    }

    #[test]
    fn rejects_hash_mismatch_before_parsing() {
        let mut request = request();
        request.expected_level_sha256 = "f".repeat(64);
        assert!(matches!(verify_level_hash(&request.expected_level_sha256, "0".repeat(64)), Err(WorkError::LevelHashMismatch { .. })));
    }

    #[test]
    fn work_list_sort_is_deterministic_and_base_filter_can_be_empty() {
        let first = list_assignment(&record(0, WORKER, 2));
        assert_eq!(first.worker_instance_id, WORKER);
        assert_eq!(first.worker_guid, WORKER_GUID);
        assert_eq!(first.work_base_id, WORK);
        assert_eq!(first.owner_map_object_concrete_model_id, OWNER);
        assert_eq!(first.fixed, 0);
        assert_eq!(first.assign_type, 7);
        assert_eq!(first.state, 3);
        let mut second = first.clone();
        second.location_index = 1;
        let mut assignments = vec![first.clone(), second.clone()];
        sort_work_list_assignments(&mut assignments);
        assert_eq!(assignments[0].location_index, 1);
        assert_eq!(assignments[1].location_index, 2);
        let empty = WorkList {
            level_sha256: "0".repeat(64),
            base_camp_id: CAMP.into(),
            work_bases: Vec::new(),
            assignments: Vec::new(),
            unscoped_assignments: Vec::new(),
        };
        assert!(empty.assignments.is_empty());
    }

    #[test]
    fn work_list_requires_canonical_lowercase_base_camp_id() {
        assert!(matches!(parse_guid("base_camp_id", &CAMP.to_uppercase()), Err(WorkError::InvalidRequest(_))));
    }
}

use std::collections::BTreeMap;
use std::fs::{self, File};

use tempfile::TempDir;
use uesave::games::palworld::Palworld;
use uesave::{
    FGuid, Header, MapEntry, PackageVersion, Properties, Property, PropertySchemas,
    PropertyTagDataPartial, PropertyTagPartial, Root, Save, SaveGameArchiveType, StructType,
    StructValue,
};

use super::*;

const A: &str = "00112233-4455-6677-8899-aabbccddeeff";
const B: &str = "11112233-4455-6677-8899-aabbccddeeff";
const INSTANCE: &str = "99112233-4455-6677-8899-aabbccddeeff";

type PalProperties = Properties<SaveGameArchiveType<Palworld>>;
type PalProperty = Property<SaveGameArchiveType<Palworld>>;

#[test]
fn rejects_case_fold_collisions() {
    let mut seen = BTreeMap::new();
    record_case_path(&mut seen, "Players/ABC.sav").unwrap();
    let error = record_case_path(&mut seen, "players/abc.sav").unwrap_err();
    assert!(matches!(error, RemapError::CaseCollision(_)));
}

#[test]
fn rejects_semantic_invariant_mismatch() {
    let before = BTreeMap::from([("Level.sav".to_owned(), "before".to_owned())]);
    let after = BTreeMap::from([("Level.sav".to_owned(), "after".to_owned())]);
    let error = verify_semantic_fingerprints(&before, &after).unwrap_err();
    assert!(matches!(error, RemapError::Gate(message) if message.contains("semantic invariant")));
}

#[test]
fn permits_host_uid_sentinel_only_in_item_container_custom_version_metadata() {
    let allowed = RewriteReport {
        opaque_candidates: vec![OpaqueCandidate {
            file: "Level.sav".to_owned(),
            path: "worldSaveData.ItemContainerSaveData[0].Value.CustomVersionData".to_owned(),
            uid: crate::host::SOURCE_HOST_UID.to_owned(),
            kind: crate::CandidateKind::Source,
            offset: 12,
        }],
        ..RewriteReport::default()
    };
    reject_opaque_candidates(&allowed).unwrap();

    let wrong_path = RewriteReport {
        opaque_candidates: vec![OpaqueCandidate {
            path: "worldSaveData.ItemContainerSaveData[0].Value.RawData".to_owned(),
            ..allowed.opaque_candidates[0].clone()
        }],
        ..RewriteReport::default()
    };
    assert!(matches!(
        reject_opaque_candidates(&wrong_path),
        Err(RemapError::OpaqueSourceReference { .. })
    ));

    let arbitrary_source = RewriteReport {
        opaque_candidates: vec![OpaqueCandidate {
            uid: A.to_owned(),
            ..allowed.opaque_candidates[0].clone()
        }],
        ..RewriteReport::default()
    };
    assert!(matches!(
        reject_opaque_candidates(&arbitrary_source),
        Err(RemapError::OpaqueSourceReference { .. })
    ));

    let target_candidate = RewriteReport {
        opaque_candidates: vec![OpaqueCandidate {
            kind: crate::CandidateKind::Target,
            ..allowed.opaque_candidates[0].clone()
        }],
        ..RewriteReport::default()
    };
    assert!(matches!(
        reject_opaque_candidates(&target_candidate),
        Err(RemapError::OpaqueTargetReference { .. })
    ));
}

#[test]
fn permits_uid_like_bytes_only_in_local_map_mask_texture() {
    let source = OpaqueCandidate {
        file: "LocalData.sav".to_owned(),
        path: "SaveData.WorldMapMaskTextureV4".to_owned(),
        uid: A.to_owned(),
        kind: crate::CandidateKind::Source,
        offset: 128,
    };
    let target = OpaqueCandidate {
        uid: B.to_owned(),
        kind: crate::CandidateKind::Target,
        ..source.clone()
    };
    let allowed = RewriteReport {
        opaque_candidates: vec![source.clone(), target],
        ..RewriteReport::default()
    };
    reject_opaque_candidates(&allowed).unwrap();

    for (file, path) in [
        ("Level.sav", "SaveData.WorldMapMaskTextureV4"),
        ("LocalData.sav", "SaveData.WorldMapMaskTextureV4.Backup"),
        ("LocalData.sav", "SaveData.WorldMapMaskTextureVersion4"),
        ("LocalData.sav", "SaveData.OtherTextureV4"),
    ] {
        let rejected = RewriteReport {
            opaque_candidates: vec![OpaqueCandidate {
                file: file.to_owned(),
                path: path.to_owned(),
                ..source.clone()
            }],
            ..RewriteReport::default()
        };
        assert!(
            matches!(
                reject_opaque_candidates(&rejected),
                Err(RemapError::OpaqueSourceReference { .. })
            ),
            "{file}:{path}"
        );
    }
}

#[test]
fn local_map_mask_versions_require_an_optional_numeric_suffix() {
    for path in [
        "SaveData.WorldMapMaskTexture",
        "SaveData.WorldMapMaskTextureV2",
        "SaveData.WorldMapMaskTextureV4",
        "SaveData.WorldMapMaskTextureV10",
    ] {
        let candidate = OpaqueCandidate {
            file: "LocalData.sav".to_owned(),
            path: path.to_owned(),
            uid: A.to_owned(),
            kind: crate::CandidateKind::Source,
            offset: 0,
        };
        assert!(is_ignorable_local_map_mask_candidate(&candidate), "{path}");
    }

    for path in [
        "SaveData.WorldMapMaskTextureV",
        "SaveData.WorldMapMaskTextureVX",
        "SaveData.WorldMapMaskTexture4",
    ] {
        let candidate = OpaqueCandidate {
            file: "LocalData.sav".to_owned(),
            path: path.to_owned(),
            uid: A.to_owned(),
            kind: crate::CandidateKind::Source,
            offset: 0,
        };
        assert!(!is_ignorable_local_map_mask_candidate(&candidate), "{path}");
    }
}

#[test]
fn custom_version_metadata_path_requires_a_numeric_item_container_index() {
    for path in [
        "worldSaveData.ItemContainerSaveData[].Value.CustomVersionData",
        "worldSaveData.ItemContainerSaveData[x].Value.CustomVersionData",
        "worldSaveData.ItemContainerSaveData[0].Value.CustomVersionData.Backup",
        "worldSaveData.OtherData[0].Value.CustomVersionData",
    ] {
        let candidate = OpaqueCandidate {
            file: "Level.sav".to_owned(),
            path: path.to_owned(),
            uid: crate::host::SOURCE_HOST_UID.to_owned(),
            kind: crate::CandidateKind::Source,
            offset: 0,
        };
        assert!(
            !is_ignorable_host_custom_version_candidate(&candidate),
            "{path}"
        );
    }
}

#[test]
fn detects_input_mutation_between_manifests_and_cleans_stage() {
    let (temp, input, output) = world();
    let mutate = input.join("notes.bin");
    let mut options = RemapOptions::default();
    options.before_second_manifest = Some(Box::new(move |_| {
        fs::write(&mutate, b"changed-after-first-manifest").unwrap();
    }));
    let error = remap_world(&input, &output, &mapping(), &options).unwrap_err();
    assert!(matches!(error, RemapError::InputChanged));
    assert!(!output.exists());
    assert_eq!(stage_count(temp.path()), 0);
}

#[test]
fn staged_reparse_failure_prevents_publish_and_cleans_stage() {
    let (temp, input, output) = world();
    let mut options = RemapOptions::default();
    options.before_validation = Some(Box::new(|stage| {
        fs::write(stage.join("Players").join(player_name(B)), b"broken-gvas").unwrap();
    }));
    let error = remap_world(&input, &output, &mapping(), &options).unwrap_err();
    assert!(matches!(error, RemapError::Parse { .. }));
    assert!(!output.exists());
    assert_eq!(stage_count(temp.path()), 0);
}

fn world() -> (TempDir, PathBuf, PathBuf) {
    let temp = TempDir::new().unwrap();
    let input = temp.path().join("world");
    let output = temp.path().join("output");
    fs::create_dir_all(input.join("Players")).unwrap();
    fs::write(input.join("notes.bin"), b"unchanged").unwrap();
    write_level(&input.join("Level.sav"));
    write_player(&input.join("Players").join(player_name(A)));
    (temp, input, output)
}

fn mapping() -> MappingSet {
    MappingSet::from_json(
        format!(r#"{{"source_uid":"{A}","target_uid":"{B}"}}"#).as_bytes(),
    )
    .unwrap()
}

fn write_player(path: &Path) {
    let mut individual = PalProperties::default();
    individual.insert("PlayerUId", guid(A));
    individual.insert("InstanceId", guid(INSTANCE));
    let mut save_data = PalProperties::default();
    save_data.insert("PlayerUId", guid(A));
    save_data.insert(
        "IndividualId",
        Property::Struct(StructValue::Struct(individual)),
    );
    let mut root = PalProperties::default();
    root.insert("SaveData", Property::Struct(StructValue::Struct(save_data)));
    let mut schemas = PropertySchemas::new();
    schemas.record("SaveData".into(), struct_tag("Synthetic.SaveData"));
    schemas.record("SaveData.PlayerUId".into(), guid_tag());
    schemas.record(
        "SaveData.IndividualId".into(),
        struct_tag("Synthetic.IndividualId"),
    );
    schemas.record("SaveData.IndividualId.PlayerUId".into(), guid_tag());
    schemas.record("SaveData.IndividualId.InstanceId".into(), guid_tag());
    write(path, root, schemas);
}

fn write_level(path: &Path) {
    let mut key = PalProperties::default();
    key.insert("PlayerUId", guid(A));
    key.insert("InstanceId", guid(INSTANCE));
    let mut world = PalProperties::default();
    world.insert(
        "CharacterSaveParameterMap",
        Property::Map(vec![MapEntry {
            key: Property::Struct(StructValue::Struct(key)),
            value: Property::Struct(StructValue::Struct(PalProperties::default())),
        }]),
    );
    let mut root = PalProperties::default();
    root.insert("worldSaveData", Property::Struct(StructValue::Struct(world)));
    let mut schemas = PropertySchemas::new();
    schemas.record("worldSaveData".into(), struct_tag("Synthetic.World"));
    schemas.record(
        "worldSaveData.CharacterSaveParameterMap".into(),
        PropertyTagPartial {
            id: None,
            data: PropertyTagDataPartial::Map {
                key_type: Box::new(struct_data("Synthetic.CharacterKey")),
                value_type: Box::new(struct_data("Synthetic.CharacterValue")),
            },
        },
    );
    schemas.record(
        "worldSaveData.CharacterSaveParameterMap.PlayerUId".into(),
        guid_tag(),
    );
    schemas.record(
        "worldSaveData.CharacterSaveParameterMap.InstanceId".into(),
        guid_tag(),
    );
    write(path, root, schemas);
}

fn write(path: &Path, properties: PalProperties, schemas: PropertySchemas) {
    let save: Save<Palworld> = Save {
        header: Header {
            magic: u32::from_le_bytes(*b"GVAS"),
            save_game_version: 2,
            package_version: PackageVersion { ue4: 522, ue5: None },
            engine_version_major: 4,
            engine_version_minor: 27,
            engine_version_patch: 0,
            engine_version_build: 0,
            engine_version: "4.27.0".into(),
            custom_version: Some((3, vec![])),
        },
        schemas,
        root: Root {
            save_game_type: "SyntheticPalworldSave".into(),
            properties,
        },
        extra: vec![],
    };
    let mut file = File::create(path).unwrap();
    save.write(&mut file).unwrap();
    file.sync_all().unwrap();
}

fn guid(value: &str) -> PalProperty {
    Property::Struct(StructValue::Guid(FGuid::parse_str(value).unwrap()))
}

fn guid_tag() -> PropertyTagPartial {
    PropertyTagPartial {
        id: None,
        data: PropertyTagDataPartial::Struct {
            struct_type: StructType::Guid,
            id: FGuid::nil(),
        },
    }
}

fn struct_tag(name: &str) -> PropertyTagPartial {
    PropertyTagPartial {
        id: None,
        data: struct_data(name),
    }
}

fn struct_data(name: &str) -> PropertyTagDataPartial {
    PropertyTagDataPartial::Struct {
        struct_type: StructType::Struct(Some(name.to_owned())),
        id: FGuid::nil(),
    }
}

fn player_name(uid: &str) -> String {
    format!("{}.sav", uid.replace('-', "").to_uppercase())
}

fn stage_count(root: &Path) -> usize {
    root.read_dir()
        .unwrap()
        .filter_map(Result::ok)
        .filter(|entry| entry.file_name().to_string_lossy().contains("palworld-uid-remap-stage"))
        .count()
}

use std::path::PathBuf;
use std::process::ExitCode;

use clap::{Parser, Subcommand};
use palworld_uid_remap::{
    analyze_host_migration, execute_host_migration, remap_world, steam_id_to_player_uid,
    analyze_work_plan, execute_work_fix_existing, MappingSet, RemapOptions, WorkRequest,
};

#[derive(Debug, Parser)]
#[command(name = "palworld-uid-remap")]
struct Args {
    #[command(subcommand)]
    command: Option<Command>,

    #[arg(long, value_name = "WORLD_DIR")]
    input: Option<PathBuf>,
    #[arg(long, value_name = "OUTPUT_DIR")]
    output: Option<PathBuf>,
    #[arg(long, value_name = "PRIVATE_JSON")]
    mapping: Option<PathBuf>,
}

#[derive(Debug, Subcommand)]
enum Command {
    Remap {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long, value_name = "OUTPUT_DIR")]
        output: PathBuf,
        #[arg(long, value_name = "PRIVATE_JSON")]
        mapping: PathBuf,
    },
    HostPlan {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long)]
        steam_id: String,
    },
    HostExecute {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long, value_name = "OUTPUT_DIR")]
        output: PathBuf,
        #[arg(long)]
        steam_id: String,
    },
    WorkPlan {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long, value_name = "REQUEST_JSON")]
        request: PathBuf,
    },
    WorkFixExisting {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long, value_name = "OUTPUT_DIR")]
        output: PathBuf,
        #[arg(long, value_name = "REQUEST_JSON")]
        request: PathBuf,
    },
    WorkList {
        #[arg(long, value_name = "WORLD_DIR")]
        input: PathBuf,
        #[arg(long, value_name = "GUID")]
        base_camp_id: String,
    },
    DeriveHostUid {
        #[arg(long)]
        steam_id: String,
    },
}

fn main() -> ExitCode {
    match run(Args::parse()) {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("palworld-uid-remap: {message}");
            ExitCode::FAILURE
        }
    }
}

fn run(args: Args) -> Result<(), String> {
    if let Some(command) = args.command {
        return match command {
            Command::Remap { input, output, mapping } => run_remap(input, output, mapping),
            Command::HostPlan { input, steam_id } => {
                write_json(&analyze_host_migration(input, &steam_id).map_err(|error| error.to_string())?)
            }
            Command::HostExecute { input, output, steam_id } => {
                write_json(&execute_host_migration(input, output, &steam_id).map_err(|error| error.to_string())?)
            }
            Command::WorkPlan { input, request } => run_work_plan(input, request),
            Command::WorkFixExisting { input, output, request } => run_work_fix(input, output, request),
            Command::WorkList { input, base_camp_id } => {
                write_json(&palworld_uid_remap::analyze_work_list(input, &base_camp_id).map_err(|error| error.to_string())?)
            }
            Command::DeriveHostUid { steam_id } => {
                let target_uid = steam_id_to_player_uid(&steam_id).map_err(|error| error.to_string())?;
                write_json(&serde_json::json!({
                    "steam_id": steam_id,
                    "target_uid": target_uid,
                }))
            }
        };
    }
    let input = args.input.ok_or_else(|| "--input is required".to_owned())?;
    let output = args.output.ok_or_else(|| "--output is required".to_owned())?;
    let mapping = args.mapping.ok_or_else(|| "--mapping is required".to_owned())?;
    run_remap(input, output, mapping)
}

fn read_work_request(path: PathBuf) -> Result<WorkRequest, String> {
    let bytes = std::fs::read(&path).map_err(|error| format!("could not read work request: {error}"))?;
    serde_json::from_slice(&bytes).map_err(|error| format!("invalid work request JSON: {error}"))
}

fn run_work_plan(input: PathBuf, request: PathBuf) -> Result<(), String> {
    let request = read_work_request(request)?;
    write_json(&analyze_work_plan(input, &request).map_err(|error| error.to_string())?)
}

fn run_work_fix(input: PathBuf, output: PathBuf, request: PathBuf) -> Result<(), String> {
    let request = read_work_request(request)?;
    write_json(&execute_work_fix_existing(input, output, &request).map_err(|error| error.to_string())?)
}

fn run_remap(input: PathBuf, output: PathBuf, mapping: PathBuf) -> Result<(), String> {
    let mapping_bytes = std::fs::read(&mapping)
        .map_err(|error| format!("could not read mapping file: {error}"))?;
    let mapping = MappingSet::from_json(&mapping_bytes).map_err(|error| error.to_string())?;
    let report = remap_world(&input, &output, &mapping, &RemapOptions::default())
        .map_err(|error| error.to_string())?;
    write_json(&report)
}

fn write_json(value: &impl serde::Serialize) -> Result<(), String> {
    serde_json::to_writer_pretty(std::io::stdout().lock(), value)
        .map_err(|error| format!("could not serialize response: {error}"))?;
    println!();
    Ok(())
}

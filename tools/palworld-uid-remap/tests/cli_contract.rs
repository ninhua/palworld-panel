use std::process::Command;

#[test]
fn requires_explicit_input_output_and_mapping_arguments() {
    let cases: &[(&[&str], &str)] = &[
        (&[], "--input"),
        (&["--input", "input"], "--output"),
        (&["--input", "input", "--output", "output"], "--mapping"),
    ];
    for &(args, required) in cases {
        let output = Command::new(env!("CARGO_BIN_EXE_palworld-uid-remap"))
            .args(args)
            .output()
            .unwrap();
        assert!(!output.status.success());
        let stderr = String::from_utf8(output.stderr).unwrap();
        assert!(stderr.contains(required), "missing {required}: {stderr}");
    }
}

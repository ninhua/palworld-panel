cmake_minimum_required(VERSION 3.22)

project(PalPanelBridge LANGUAGES CXX)

set(PALPANEL_UE4SS_ROOT "" CACHE PATH "Path to the pinned RE-UE4SS source tree")
if(NOT EXISTS "${PALPANEL_UE4SS_ROOT}/CMakeLists.txt")
  message(FATAL_ERROR "PALPANEL_UE4SS_ROOT must point to RE-UE4SS v3.0.1")
endif()

add_subdirectory("${PALPANEL_UE4SS_ROOT}" "${CMAKE_BINARY_DIR}/ue4ss")

add_library(PalPanelBridge SHARED src/main.cpp)
target_compile_features(PalPanelBridge PRIVATE cxx_std_20)
target_compile_definitions(PalPanelBridge PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(PalPanelBridge PRIVATE UE4SS ws2_32)
set_target_properties(PalPanelBridge PROPERTIES
  OUTPUT_NAME "main"
  RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
  RUNTIME_OUTPUT_DIRECTORY_RELEASE "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
  RUNTIME_OUTPUT_DIRECTORY_GAME__SHIPPING__WIN64 "${CMAKE_BINARY_DIR}/artifact/PalPanelBridge/dlls"
)

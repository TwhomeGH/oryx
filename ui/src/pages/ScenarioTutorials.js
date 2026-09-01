//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from "react";
import {TutorialsToast, useTutorials} from "../components/TutorialsButton";

export default function ScenarioTutorials() {
  const movieTutorials = useTutorials('all');

  return (
      <TutorialsToast tutorials={movieTutorials} />
  );
}

